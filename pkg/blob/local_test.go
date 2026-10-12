package blob_test

import (
	"testing"

	"github.com/ACM-VIT/valence/pkg/blob"
	"github.com/ACM-VIT/valence/pkg/blob/blobtest"
)

func TestLocal(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blob.Store {
		s, err := blob.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}
