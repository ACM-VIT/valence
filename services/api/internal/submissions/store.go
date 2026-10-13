package submissions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
)

var (
	ErrBadTransition = errors.New("submissions: status change not allowed")
	ErrStale         = errors.New("submissions: attempt changed underneath")
	ErrNoLease       = errors.New("submissions: lease token required")
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// SetStatus moves an attempt from one status to another for the worker
// holding the queue lease token. Moving to leased records the token; every
// other change, and Finalize, must carry the same token, so a worker whose
// lease expired can't write over its replacement.
func (s *Store) SetStatus(ctx context.Context, id int64, attempt int32, token string, from, to db.SubmissionStatus, currentTest *int32) error {
	if to == db.SubmissionStatusFinalized || !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrBadTransition, from, to)
	}
	if token == "" {
		return ErrNoLease
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		var n int64
		var err error
		if to == db.SubmissionStatusLeased {
			n, err = q.ClaimAttempt(ctx, db.ClaimAttemptParams{
				SubmissionID: id, Attempt: attempt, FromStatus: from, LeaseToken: token,
			})
		} else {
			n, err = q.SetAttemptStatus(ctx, db.SetAttemptStatusParams{
				SubmissionID: id, Attempt: attempt, FromStatus: from, ToStatus: to, LeaseToken: token,
			})
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrStale
		}
		return q.SetSubmissionStatus(ctx, db.SetSubmissionStatusParams{
			ID: id, Attempt: attempt, Status: to, CurrentTest: currentTest,
		})
	})
}

type Result struct {
	Verdict       db.Verdict
	TimeMS        *int32
	MemoryKiB     *int32
	CompileOutput *string
	Tests         []TestResult
}

type TestResult struct {
	Ordinal   int32
	Verdict   db.Verdict
	TimeMS    int32
	MemoryKiB int32
}

// Finalize saves the verdict if token still owns the attempt and it isn't
// finalized yet. It reports false, with no error, when nothing changed: the
// attempt was already finalized, never leased, or leased by another worker.
func (s *Store) Finalize(ctx context.Context, id int64, attempt int32, token string, r Result) (bool, error) {
	if token == "" {
		return false, ErrNoLease
	}
	var done bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		n, err := q.FinalizeAttempt(ctx, db.FinalizeAttemptParams{
			SubmissionID: id, Attempt: attempt, LeaseToken: token, Verdict: r.Verdict,
			TimeMs: r.TimeMS, MemoryKib: r.MemoryKiB, CompileOutput: r.CompileOutput,
		})
		if err != nil || n == 0 {
			return err
		}

		p := db.InsertTestResultsParams{SubmissionID: id, Attempt: attempt}
		for _, t := range r.Tests {
			p.Ordinals = append(p.Ordinals, t.Ordinal)
			p.Verdicts = append(p.Verdicts, string(t.Verdict))
			p.TimesMs = append(p.TimesMs, t.TimeMS)
			p.MemoriesKib = append(p.MemoriesKib, t.MemoryKiB)
		}
		if err := q.InsertTestResults(ctx, p); err != nil {
			return err
		}
		done = true
		return q.FinalizeSubmission(ctx, db.FinalizeSubmissionParams{
			ID: id, Attempt: attempt, Verdict: r.Verdict, TimeMs: r.TimeMS, MemoryKib: r.MemoryKiB,
		})
	})
	return done && err == nil, err
}
