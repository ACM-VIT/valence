package submissions

import (
	"context"
	"crypto/rand"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
)

// setup connects to DATABASE_URL and creates a throwaway user and problem,
// deleted again when the test ends.
func setup(t *testing.T) (*pgxpool.Pool, int64, int64) {
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

	name := "test-" + strings.ToLower(rand.Text()[:10])
	var userID, problemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (handle) VALUES ($1) RETURNING id`, name).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO problems (slug, title) VALUES ($1, 'T') RETURNING id`, name).Scan(&problemID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM submissions WHERE user_id = $1`, userID)
		pool.Exec(ctx, `DELETE FROM problems WHERE id = $1`, problemID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
	return pool, userID, problemID
}

// submit creates a submission with its first attempt, as SubmitSolution will.
func submit(t *testing.T, q *db.Queries, userID, problemID int64) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := q.CreateSubmission(ctx, db.CreateSubmissionParams{
		UserID: userID, ProblemID: problemID, ProblemRevision: 1, Language: "cpp17", Source: "int main() {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateAttempt(ctx, db.CreateAttemptParams{SubmissionID: id, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func TestJudgingLifecycle(t *testing.T) {
	pool, userID, problemID := setup(t)
	ctx := context.Background()
	s, q := NewStore(pool), db.New(pool)
	id := submit(t, q, userID, problemID)

	// A worker takes the job and judges test 1, then test 2.
	steps := []struct {
		from, to db.SubmissionStatus
		test     *int32
	}{
		{db.SubmissionStatusQueued, db.SubmissionStatusLeased, nil},
		{db.SubmissionStatusLeased, db.SubmissionStatusCompiling, nil},
		{db.SubmissionStatusCompiling, db.SubmissionStatusRunning, ptr[int32](1)},
		{db.SubmissionStatusRunning, db.SubmissionStatusChecking, ptr[int32](1)},
		{db.SubmissionStatusChecking, db.SubmissionStatusRunning, ptr[int32](2)},
	}
	for _, st := range steps {
		if err := s.SetStatus(ctx, id, 1, st.from, st.to, st.test); err != nil {
			t.Fatalf("%s → %s: %v", st.from, st.to, err)
		}
	}
	sub, _ := q.GetSubmission(ctx, id)
	if sub.Status != db.SubmissionStatusRunning || sub.CurrentTest == nil || *sub.CurrentTest != 2 {
		t.Fatalf("submission is %s on test %v, want running on test 2", sub.Status, sub.CurrentTest)
	}

	// Rule-breaking and out-of-date changes are refused.
	if err := s.SetStatus(ctx, id, 1, db.SubmissionStatusRunning, db.SubmissionStatusQueued, nil); !errors.Is(err, ErrBadTransition) {
		t.Errorf("running → queued: got %v, want ErrBadTransition", err)
	}
	if err := s.SetStatus(ctx, id, 1, db.SubmissionStatusCompiling, db.SubmissionStatusRunning, nil); !errors.Is(err, ErrStale) {
		t.Errorf("stale from-status: got %v, want ErrStale", err)
	}

	// Test 2 fails: Wrong Answer.
	wa := Result{Verdict: db.VerdictWA, TimeMS: ptr[int32](120), MemoryKiB: ptr[int32](2048), Tests: []TestResult{
		{Ordinal: 1, Verdict: db.VerdictAC, TimeMS: 100, MemoryKiB: 2000},
		{Ordinal: 2, Verdict: db.VerdictWA, TimeMS: 120, MemoryKiB: 2048},
	}}
	if done, err := s.Finalize(ctx, id, 1, wa); err != nil || !done {
		t.Fatalf("Finalize = %v, %v; want true, nil", done, err)
	}

	// The worker retries its report with different results: nothing changes.
	ac := Result{Verdict: db.VerdictAC, Tests: []TestResult{{Ordinal: 3, Verdict: db.VerdictAC}}}
	if done, err := s.Finalize(ctx, id, 1, ac); err != nil || done {
		t.Fatalf("second Finalize = %v, %v; want false, nil", done, err)
	}

	sub, _ = q.GetSubmission(ctx, id)
	if sub.Status != db.SubmissionStatusFinalized || !sub.Verdict.Valid || sub.Verdict.Verdict != db.VerdictWA ||
		sub.TimeMs == nil || *sub.TimeMs != 120 || sub.CurrentTest != nil || sub.JudgedAt == nil {
		t.Fatalf("submission after finalize: %+v", sub)
	}
	results, _ := q.ListTestResults(ctx, db.ListTestResultsParams{SubmissionID: id, Attempt: 1})
	if len(results) != 2 || results[1].Verdict != db.VerdictWA {
		t.Fatalf("test results = %+v, want 2 with test 2 WA", results)
	}

	// Finalized is the end.
	if err := s.SetStatus(ctx, id, 1, db.SubmissionStatusFinalized, db.SubmissionStatusLeased, nil); !errors.Is(err, ErrBadTransition) {
		t.Errorf("finalized → leased: got %v, want ErrBadTransition", err)
	}
}

func TestListUserSubmissions(t *testing.T) {
	pool, userID, problemID := setup(t)
	ctx := context.Background()
	q := db.New(pool)
	var ids []int64
	for range 3 {
		ids = append(ids, submit(t, q, userID, problemID))
	}

	// Newest first, two per page.
	page1, err := q.ListUserSubmissions(ctx, db.ListUserSubmissionsParams{UserID: userID, BeforeID: math.MaxInt64, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || page1[0].ID != ids[2] || page1[1].ID != ids[1] {
		t.Fatalf("page 1 = %+v, want submissions %d and %d", page1, ids[2], ids[1])
	}
	page2, _ := q.ListUserSubmissions(ctx, db.ListUserSubmissionsParams{UserID: userID, BeforeID: page1[1].ID, PageSize: 2})
	if len(page2) != 1 || page2[0].ID != ids[0] {
		t.Fatalf("page 2 = %+v, want submission %d", page2, ids[0])
	}

	// Filtering by problem.
	other := problemID + 1_000_000
	none, _ := q.ListUserSubmissions(ctx, db.ListUserSubmissionsParams{UserID: userID, ProblemID: &other, BeforeID: math.MaxInt64, PageSize: 10})
	mine, _ := q.ListUserSubmissions(ctx, db.ListUserSubmissionsParams{UserID: userID, ProblemID: &problemID, BeforeID: math.MaxInt64, PageSize: 10})
	if len(none) != 0 || len(mine) != 3 {
		t.Fatalf("filtered by problem: %d for another problem, %d for this one; want 0 and 3", len(none), len(mine))
	}
}
