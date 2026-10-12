package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o755); err != nil {
		return nil, fmt.Errorf("blob: create root: %w", err)
	}
	return &Local{root: root}, nil
}

var keyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (l *Local) path(key Key) (string, error) {
	k := string(key)
	if !keyPattern.MatchString(k) {
		return "", fmt.Errorf("blob: invalid key %q", k)
	}
	return filepath.Join(l.root, k[0:2], k[2:4], k), nil
}

func (l *Local) Put(ctx context.Context, r io.Reader) (Key, int64, error) {
	tmp, err := os.CreateTemp(filepath.Join(l.root, "tmp"), "put-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}

	key := Key(hex.EncodeToString(h.Sum(nil)))
	dst, _ := l.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", 0, err
	}
	return key, n, nil
}

func (l *Local) Get(ctx context.Context, key Key) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l *Local) Exists(ctx context.Context, key Key) (bool, error) {
	p, err := l.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (l *Local) PresignGet(ctx context.Context, key Key, ttl time.Duration) (string, error) {
	return "", ErrNotSupported
}

var _ Store = (*Local)(nil)
