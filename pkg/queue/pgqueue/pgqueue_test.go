package pgqueue

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/pkg/queue/queuetest"
)

// TestConformance uses a one-connection pool: idle Lease calls must not hold
// it, or Enqueue could never insert the job that wakes them.
func TestConformance(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := New(pool)
	t.Cleanup(q.Close)
	queuetest.Run(t, q)
}
