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

// Claim moves an attempt to leased for the worker holding a queue lease and
// records the lease's token and generation (queue.Lease.Attempt, which only
// grows). A claim with a generation no newer than the last claim is refused
// with ErrStale, so a delayed claim from an expired lease can't take the
// attempt back from the worker that replaced it.
func (s *Store) Claim(ctx context.Context, id int64, attempt int32, token string, generation int32, from db.SubmissionStatus) error {
	if !CanTransition(from, db.SubmissionStatusLeased) {
		return fmt.Errorf("%w: %s -> %s", ErrBadTransition, from, db.SubmissionStatusLeased)
	}
	if token == "" || generation < 1 {
		return ErrNoLease
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		n, err := q.ClaimAttempt(ctx, db.ClaimAttemptParams{
			SubmissionID: id, Attempt: attempt, FromStatus: from, LeaseToken: token, LeaseGeneration: generation,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrStale
		}
		return q.SetSubmissionStatus(ctx, db.SetSubmissionStatusParams{
			ID: id, Attempt: attempt, Status: db.SubmissionStatusLeased,
		})
	})
}

// SetStatus moves a claimed attempt from one status to another. It must
// carry the token recorded by Claim, as must Finalize, so a worker whose
// lease expired can't write over its replacement. Use Claim to move to leased.
func (s *Store) SetStatus(ctx context.Context, id int64, attempt int32, token string, from, to db.SubmissionStatus, currentTest *int32) error {
	if to == db.SubmissionStatusFinalized || to == db.SubmissionStatusLeased || !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrBadTransition, from, to)
	}
	if token == "" {
		return ErrNoLease
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		n, err := q.SetAttemptStatus(ctx, db.SetAttemptStatusParams{
			SubmissionID: id, Attempt: attempt, FromStatus: from, ToStatus: to, LeaseToken: token,
		})
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
