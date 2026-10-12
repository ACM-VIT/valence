package problems

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
)

func TestReplaceTests(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// A throwaway problem; deleting it cascades to its tests.
	slug := "test-" + strings.ToLower(rand.Text()[:10])
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO problems (slug, title) VALUES ($1, 'Test') RETURNING id`, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM problems WHERE id = $1`, id) })

	s, q := NewStore(pool), db.New(pool)
	hash := strings.Repeat("a", 64)
	test := func(ordinal int32, sample bool) db.InsertTestsParams {
		return db.InsertTestsParams{Ordinal: ordinal, InputHash: hash, OutputHash: hash, IsSample: sample}
	}

	// 1. Three tests, one of them a sample.
	if err := s.ReplaceTests(ctx, id, []db.InsertTestsParams{test(1, true), test(2, false), test(3, false)}); err != nil {
		t.Fatal(err)
	}
	all, _ := q.ListTests(ctx, id)
	samples, _ := q.ListSampleTests(ctx, id)
	if len(all) != 3 || len(samples) != 1 || all[0].Ordinal != 1 {
		t.Fatalf("after first replace: %d tests, %d samples", len(all), len(samples))
	}

	// 2. Replacing with two tests leaves exactly two.
	if err := s.ReplaceTests(ctx, id, []db.InsertTestsParams{test(1, true), test(2, false)}); err != nil {
		t.Fatal(err)
	}
	if all, _ = q.ListTests(ctx, id); len(all) != 2 {
		t.Fatalf("after second replace: %d tests, want 2", len(all))
	}

	// 3. A failing replace (duplicate ordinal) must leave the old tests alone.
	if err := s.ReplaceTests(ctx, id, []db.InsertTestsParams{test(1, false), test(1, false)}); err == nil {
		t.Fatal("duplicate ordinals were accepted")
	}
	if all, _ = q.ListTests(ctx, id); len(all) != 2 {
		t.Fatalf("failed replace changed the tests: %d left, want 2", len(all))
	}
}
