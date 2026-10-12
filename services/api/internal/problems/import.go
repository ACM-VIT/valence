package problems

import (
	"context"
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

func putFile(ctx context.Context, blobs blob.Store, path string) (blob.Key, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	return blobs.Put(ctx, f)
}
