package problems

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ACM-VIT/valence/gen/go/db"
	"github.com/ACM-VIT/valence/pkg/blob"
)

func TestPutFileRejectsLinksAndHugeFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	blobs, err := blob.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	// files counts what's in the blob store, temp files included.
	files := func() int {
		n := 0
		filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n
	}

	link := filepath.Join(dir, "01.in")
	if err := os.Symlink("/dev/zero", link); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	if _, _, err := putFile(ctx, blobs, link); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink to /dev/zero: got %v, want a not-a-regular-file error", err)
	}

	// A file that grew past the limit after validation: copying fails there
	// and nothing is kept, not even a partial copy or a temp file.
	big := filepath.Join(dir, "02.in")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTestFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err := putFile(ctx, blobs, big); !errors.Is(err, errTestTooLarge) {
		t.Errorf("file over the limit: got %v, want errTestTooLarge", err)
	}
	if n := files(); n != 0 {
		t.Errorf("blob store has %d files after a rejected put, want 0", n)
	}

	// The text check during validation is bounded the same way.
	if msg := checkTestText(big); msg != "larger than 64 MiB" {
		t.Errorf("checkTestText on an oversized file = %q, want the size error", msg)
	}

	ok := filepath.Join(dir, "03.in")
	os.WriteFile(ok, []byte("1 2\n"), 0o644)
	if _, n, err := putFile(ctx, blobs, ok); err != nil || n != 4 {
		t.Errorf("normal file: n=%d err=%v", n, err)
	}
}

// Imports that race to create the same new slug must all succeed: one
// creates the problem and the rest see it as already up to date.
func TestUpsertConcurrentNewSlug(t *testing.T) {
	pool, _ := testProblem(t) // only for the pool; that problem is unused
	ctx := context.Background()
	s := NewStore(pool)

	for round := range 5 {
		slug := "test-" + strings.ToLower(rand.Text()[:10])
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM problems WHERE slug = $1`, slug) })
		p := db.CreateProblemParams{Slug: slug, Title: "T", TimeLimitMs: 1000, MemoryLimitKib: 262144, Checker: "tokens"}
		hash := strings.Repeat("b", 64)
		tests := []db.InsertTestsParams{{Ordinal: 1, InputHash: hash, OutputHash: hash, IsSample: true}}

		const n = 8
		results := make([]ImportResult, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], _, errs[i] = s.Upsert(ctx, p, tests)
			}()
		}
		wg.Wait()

		count := map[ImportResult]int{}
		for i := range n {
			if errs[i] != nil {
				t.Fatalf("round %d, import %d: %v", round, i, errs[i])
			}
			count[results[i]]++
		}
		if count[Created] != 1 || count[Unchanged] != n-1 {
			t.Fatalf("round %d: %v, want 1 created and %d unchanged", round, fmt.Sprint(count), n-1)
		}
	}
}
