package problems

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
	v1 "github.com/ACM-VIT/valence/gen/go/valence/v1"
	"github.com/ACM-VIT/valence/pkg/blob"
	"github.com/ACM-VIT/valence/services/api/internal/auth"
)

// newTestService connects to DATABASE_URL and returns a Service plus a
// prefix that every problem the test creates should use; they are
// deleted when the test ends.
func newTestService(t *testing.T) (*Service, *pgxpool.Pool, blob.Store, string) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	blobs, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := "test-" + strings.ToLower(rand.Text()[:8]) + "-"
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM problems WHERE slug LIKE $1`, prefix+"%")
	})
	return NewService(db.New(pool), blobs), pool, blobs, prefix
}

func addProblem(t *testing.T, pool *pgxpool.Pool, slug, visibility string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO problems (slug, title, visibility) VALUES ($1, $1, $2) RETURNING id`,
		slug, visibility).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var (
	anonymous = context.Background()
	setter    = auth.NewContext(context.Background(), auth.User{ID: 1, Role: "setter"})
)

// listAll follows next_page_token until the last page and returns every slug.
func listAll(t *testing.T, s *Service, ctx context.Context, pageSize int32) []string {
	t.Helper()
	var slugs []string
	token := ""
	for pages := 0; ; pages++ {
		if pages > 1000 {
			t.Fatal("pagination never ended")
		}
		res, err := s.ListProblems(ctx, connect.NewRequest(&v1.ListProblemsRequest{PageSize: pageSize, PageToken: token}))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Msg.Problems) > int(pageSize) {
			t.Fatalf("got %d problems, page size is %d", len(res.Msg.Problems), pageSize)
		}
		for _, p := range res.Msg.Problems {
			slugs = append(slugs, p.Slug)
		}
		if token = res.Msg.NextPageToken; token == "" {
			return slugs
		}
	}
}

func TestListProblemsPagination(t *testing.T) {
	s, pool, _, prefix := newTestService(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		addProblem(t, pool, prefix+name, "public")
	}
	addProblem(t, pool, prefix+"secret", "private")

	// Only this test's problems, in the order they come back.
	mine := func(slugs []string) string {
		var out []string
		for _, s := range slugs {
			if strings.HasPrefix(s, prefix) {
				out = append(out, strings.TrimPrefix(s, prefix))
			}
		}
		return strings.Join(out, " ")
	}

	// Pages of 2 must return every public problem exactly once, in order.
	if got := mine(listAll(t, s, anonymous, 2)); got != "a b c d e" {
		t.Errorf("anonymous sees %q, want %q", got, "a b c d e")
	}
	if got := mine(listAll(t, s, setter, 2)); got != "a b c d e secret" {
		t.Errorf("setter sees %q, want %q", got, "a b c d e secret")
	}

	_, err := s.ListProblems(anonymous, connect.NewRequest(&v1.ListProblemsRequest{PageToken: "not-a-number"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("bad page token: got %v, want InvalidArgument", err)
	}
}

func TestGetProblemHidesHiddenTests(t *testing.T) {
	s, pool, blobs, prefix := newTestService(t)
	ctx := context.Background()

	put := func(content string) (string, int64) {
		key, size, err := blobs.Put(ctx, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		return string(key), size
	}
	id := addProblem(t, pool, prefix+"p", "public")
	var rows []db.InsertTestsParams
	for i, name := range []string{"sample", "hidden"} {
		inHash, inSize := put(name + "-input")
		outHash, outSize := put(name + "-output")
		rows = append(rows, db.InsertTestsParams{
			Ordinal: int32(i + 1), InputHash: inHash, InputSize: inSize,
			OutputHash: outHash, OutputSize: outSize, IsSample: name == "sample",
		})
	}
	if err := NewStore(pool).ReplaceTests(ctx, id, rows); err != nil {
		t.Fatal(err)
	}

	res, err := s.GetProblem(anonymous, connect.NewRequest(&v1.GetProblemRequest{Slug: prefix + "p"}))
	if err != nil {
		t.Fatal(err)
	}
	samples := res.Msg.Problem.Samples
	if len(samples) != 1 || samples[0].Input != "sample-input" || samples[0].Output != "sample-output" {
		t.Fatalf("samples = %v, want only the sample test", samples)
	}
	if strings.Contains(res.Msg.String(), "hidden") {
		t.Fatal("the response contains a hidden test")
	}
}

func TestGetPrivateProblem(t *testing.T) {
	s, pool, _, prefix := newTestService(t)
	addProblem(t, pool, prefix+"secret", "private")
	req := connect.NewRequest(&v1.GetProblemRequest{Slug: prefix + "secret"})

	// To a contestant, a private problem doesn't exist.
	if _, err := s.GetProblem(anonymous, req); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("anonymous: got %v, want NotFound", err)
	}
	if _, err := s.GetProblem(setter, req); err != nil {
		t.Errorf("setter: %v", err)
	}
	missing := connect.NewRequest(&v1.GetProblemRequest{Slug: prefix + "nope"})
	if _, err := s.GetProblem(setter, missing); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("missing problem: got %v, want NotFound", err)
	}
}
