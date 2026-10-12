package problems

import (
	"context"

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
		q := db.New(tx)
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
	})
}
