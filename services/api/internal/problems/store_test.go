package problems

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
)

// testProblem connects to DATABASE_URL and creates a throwaway problem;
// deleting it at the end cascades to its tests.
func testProblem(t *testing.T) (*pgxpool.Pool, int64) {
	t.Helper()
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

	slug := "test-" + strings.ToLower(rand.Text()[:10])
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO problems (slug, title) VALUES ($1, 'Test') RETURNING id`, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM problems WHERE id = $1`, id) })
	return pool, id
}

func TestReplaceTests(t *testing.T) {
	pool, id := testProblem(t)
	ctx := context.Background()

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

// Replacements for the same problem run one at a time: they all succeed, and
// the result is exactly one caller's test set, never a mix.
func TestReplaceTestsConcurrent(t *testing.T) {
	pool, id := testProblem(t)
	ctx := context.Background()
	s, q := NewStore(pool), db.New(pool)

	const writers = 8
	sets := make([][]db.InsertTestsParams, writers)
	for w := range writers {
		hash := fmt.Sprintf("%064x", w+1) // tells the writers apart
		for n := range w + 1 {            // writer w saves tests 1..w+1
			sets[w] = append(sets[w], db.InsertTestsParams{Ordinal: int32(n + 1), InputHash: hash, OutputHash: hash})
		}
	}

	for round := range 5 {
		var wg sync.WaitGroup
		errs := make([]error, writers)
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[w] = s.ReplaceTests(ctx, id, sets[w])
			}()
		}
		wg.Wait()
		for w, err := range errs {
			if err != nil {
				t.Fatalf("round %d: writer %d: %v", round, w, err)
			}
		}

		got, err := q.ListTests(ctx, id)
		if err != nil || len(got) == 0 {
			t.Fatalf("round %d: %d tests, err %v", round, len(got), err)
		}
		winner := got[0].InputHash
		want := sets[int(got[0].InputHash[63]-'0')-1]
		if len(got) != len(want) {
			t.Fatalf("round %d: %d tests, want writer %s's %d", round, len(got), winner, len(want))
		}
		for _, row := range got {
			if row.InputHash != winner {
				t.Fatalf("round %d: tests from different writers were mixed", round)
			}
		}
	}
}
