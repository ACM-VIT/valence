package blobtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ACM-VIT/valence/pkg/blob"
)

func Run(t *testing.T, newStore func(t *testing.T) blob.Store) {
	ctx := context.Background()

	t.Run("put then get returns the same bytes", func(t *testing.T) {
		s := newStore(t)
		data := []byte("hello, valence\n")
		key, n, err := s.Put(ctx, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if want := blob.Key(hex.EncodeToString(sum[:])); key != want {
			t.Fatalf("key = %s, want sha256 %s", key, want)
		}
		if n != int64(len(data)) {
			t.Fatalf("size = %d, want %d", n, len(data))
		}
		if got := read(t, s, key); !bytes.Equal(got, data) {
			t.Fatalf("got %q, want %q", got, data)
		}
	})

	t.Run("empty blob", func(t *testing.T) {
		s := newStore(t)
		key, n, err := s.Put(ctx, strings.NewReader(""))
		if err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		if got := read(t, s, key); len(got) != 0 {
			t.Fatalf("got %d bytes, want 0", len(got))
		}
	})

	t.Run("same content twice gives the same key", func(t *testing.T) {
		s := newStore(t)
		k1, _, err1 := s.Put(ctx, strings.NewReader("same"))
		k2, _, err2 := s.Put(ctx, strings.NewReader("same"))
		if err1 != nil || err2 != nil || k1 != k2 {
			t.Fatalf("k1=%s k2=%s err1=%v err2=%v", k1, k2, err1, err2)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		s := newStore(t)
		missing := blob.Key(strings.Repeat("0", 64))
		if _, err := s.Get(ctx, missing); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("Get err = %v, want ErrNotFound", err)
		}
		if ok, err := s.Exists(ctx, missing); ok || err != nil {
			t.Fatalf("Exists = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("exists after put", func(t *testing.T) {
		s := newStore(t)
		key, _, err := s.Put(ctx, strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := s.Exists(ctx, key); !ok || err != nil {
			t.Fatalf("Exists = %v, %v; want true, nil", ok, err)
		}
	})

	t.Run("rejects keys that are not hashes", func(t *testing.T) {
		s := newStore(t)
		bad := []blob.Key{"", "../../etc/passwd", "ABCDEF", blob.Key(strings.Repeat("g", 64))}
		for _, k := range bad {
			if _, err := s.Get(ctx, k); err == nil {
				t.Errorf("Get(%q) succeeded, want an error", k)
			}
		}
	})

	t.Run("concurrent puts of the same content", func(t *testing.T) {
		s := newStore(t)
		data := bytes.Repeat([]byte("valence"), 100_000)
		var wg sync.WaitGroup
		keys := make([]blob.Key, 8)
		errs := make([]error, 8)
		for i := range keys {
			wg.Add(1)
			go func() {
				defer wg.Done()
				keys[i], _, errs[i] = s.Put(ctx, bytes.NewReader(data))
			}()
		}
		wg.Wait()
		for i := range keys {
			if errs[i] != nil || keys[i] != keys[0] {
				t.Fatalf("put %d: key=%s err=%v", i, keys[i], errs[i])
			}
		}
		if got := read(t, s, keys[0]); !bytes.Equal(got, data) {
			t.Fatal("content corrupted by concurrent puts")
		}
	})
}

func read(t *testing.T, s blob.Store, key blob.Key) []byte {
	t.Helper()
	rc, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}