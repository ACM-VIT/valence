package problems

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ACM-VIT/valence/gen/go/db"
	"github.com/ACM-VIT/valence/pkg/blob"
)

func (s *Store) Import(ctx context.Context, blobs blob.Store, p *Package) (ImportResult, int32, error) {

	tests := make([]db.InsertTestsParams, len(p.Tests))
	for i, t := range p.Tests {
		inKey, inSize, err := putFile(ctx, blobs, t.In)
		if err != nil {
			return "", 0, err
		}
		outKey, outSize, err := putFile(ctx, blobs, t.Out)
		if err != nil {
			return "", 0, err
		}
		tests[i] = db.InsertTestsParams{
			Ordinal:    t.Ordinal,
			InputHash:  string(inKey),
			InputSize:  inSize,
			OutputHash: string(outKey),
			OutputSize: outSize,
			IsSample:   t.Sample,
		}
	}

	return s.Upsert(ctx, db.CreateProblemParams{
		Slug:           p.Slug,
		Title:          p.Title,
		StatementMd:    p.Statement,
		TimeLimitMs:    p.TimeLimitMS,
		MemoryLimitKib: p.MemoryLimitKiB,
		Checker:        p.Checker,
	}, tests)
}

var errTestTooLarge = errors.New("larger than 64 MiB")

// putFile stores one test file. It re-checks what Load checked, because the
// file may have changed since: it must still be a regular file, and reading
// fails as soon as it passes maxTestFileBytes, so the store discards the
// partial copy instead of saving it.
func putFile(ctx context.Context, blobs blob.Store, path string) (blob.Key, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s: not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	key, n, err := blobs.Put(ctx, &maxReader{r: f, left: maxTestFileBytes})
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", path, err)
	}
	return key, n, nil
}

// maxReader reads from r but returns errTestTooLarge once more than left
// bytes have come through.
type maxReader struct {
	r    io.Reader
	left int64
}

func (m *maxReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	m.left -= int64(n)
	if m.left < 0 {
		return n, errTestTooLarge
	}
	return n, err
}
