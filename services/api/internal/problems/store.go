package problems

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ReplaceTests(ctx context.Context, problemID int64, tests []db.InsertTestsParams) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return replaceTests(ctx, db.New(tx), problemID, tests)
	})
}

func replaceTests(ctx context.Context, q *db.Queries, problemID int64, tests []db.InsertTestsParams) error {
	// Lock the problem first, so concurrent replacements run one at a
	// time instead of interleaving their deletes and inserts.
	if _, err := q.LockProblem(ctx, problemID); err != nil {
		return err
	}
	if err := q.DeleteTests(ctx, problemID); err != nil {
		return err
	}
	rows := make([]db.InsertTestsParams, len(tests))
	for i, t := range tests {
		t.ProblemID = problemID
		rows[i] = t
	}
	_, err := q.InsertTests(ctx, rows)
	return err
}

type ImportResult string

const (
	Created   ImportResult = "created"
	Updated   ImportResult = "updated"
	Unchanged ImportResult = "unchanged"
)

func (s *Store) Upsert(ctx context.Context, p db.CreateProblemParams, tests []db.InsertTestsParams) (result ImportResult, revision int32, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		old, err := q.GetProblemBySlug(ctx, p.Slug)
		if errors.Is(err, pgx.ErrNoRows) {
			id, err := q.CreateProblem(ctx, p)
			if err != nil {
				return err
			}
			result, revision = Created, 1
			return replaceTests(ctx, q, id, tests)
		}
		if err != nil {
			return err
		}
		// Lock, then read again: a concurrent import may have changed the
		// problem between the first read and the lock.
		if _, err := q.LockProblem(ctx, old.ID); err != nil {
			return err
		}
		if old, err = q.GetProblemBySlug(ctx, p.Slug); err != nil {
			return err
		}

		oldTests, err := q.ListTests(ctx, old.ID)
		if err != nil {
			return err
		}
		if sameProblem(old, p) && sameTests(oldTests, tests) {
			result, revision = Unchanged, old.Revision
			return nil
		}

		revision, err = q.UpdateProblem(ctx, db.UpdateProblemParams{
			ID:             old.ID,
			Title:          p.Title,
			StatementMd:    p.StatementMd,
			TimeLimitMs:    p.TimeLimitMs,
			MemoryLimitKib: p.MemoryLimitKib,
			Checker:        p.Checker,
		})
		if err != nil {
			return err
		}
		result = Updated
		return replaceTests(ctx, q, old.ID, tests)
	})
	return result, revision, err
}

func sameProblem(old db.Problem, p db.CreateProblemParams) bool {
	return old.Title == p.Title && old.StatementMd == p.StatementMd && old.TimeLimitMs == p.TimeLimitMs && old.MemoryLimitKib == p.MemoryLimitKib && old.Checker == p.Checker
}

func sameTests(old []db.Test, tests []db.InsertTestsParams) bool {
	if len(old) != len(tests) {
		return false
	}

	for i, t := range tests {
		o := old[i]
		if o.Ordinal != t.Ordinal || o.InputHash != t.InputHash || o.InputSize != t.InputSize || o.OutputHash != t.OutputHash || o.OutputSize != t.OutputSize || o.IsSample != t.IsSample {
			return false
		}
	}
	return true
}
