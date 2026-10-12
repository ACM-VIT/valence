package problems

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
	"github.com/ACM-VIT/valence/pkg/blob"
)

func TestImportIsIdempotent(t *testing.T) {
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
	blobs, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(pool)

	// A copy of aplusb under a random slug, so the test never touches real problems.
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../../../problems/examples/aplusb")); err != nil {
		t.Fatal(err)
	}
	slug := "test-" + strings.ToLower(rand.Text()[:10])
	yamlPath := filepath.Join(dir, "problem.yaml")
	raw, _ := os.ReadFile(yamlPath)
	if !strings.Contains(string(raw), "slug: aplusb") {
		t.Fatal("expected aplusb's problem.yaml to contain `slug: aplusb`")
	}
	os.WriteFile(yamlPath, []byte(strings.Replace(string(raw), "slug: aplusb", "slug: "+slug, 1)), 0o644)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM problems WHERE slug = $1`, slug) })

	importDir := func(want ImportResult, wantRevision int32) {
		t.Helper()
		p, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		result, revision, err := s.Import(ctx, blobs, p)
		if err != nil {
			t.Fatal(err)
		}
		if result != want || revision != wantRevision {
			t.Fatalf("got %s (revision %d), want %s (revision %d)", result, revision, want, wantRevision)
		}
	}

	importDir(Created, 1)
	importDir(Unchanged, 1) // same package again: nothing changes

	// Change one expected output: the revision goes up and the new file is stored.
	if err := os.WriteFile(filepath.Join(dir, "tests", "01.out"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	importDir(Updated, 2)

	problem, err := db.New(pool).GetProblemBySlug(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	tests, _ := db.New(pool).ListTests(ctx, problem.ID)
	if ok, _ := blobs.Exists(ctx, blob.Key(tests[0].OutputHash)); !ok {
		t.Error("the new 01.out isn't in the blob store")
	}
}
