package pgqueue

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// syncBuffer is a bytes.Buffer safe for the listener goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// When the listener can't connect, it says so instead of failing silently.
func TestListenerErrorsAreLogged(t *testing.T) {
	// Nothing listens on port 1; the pool itself never connects here.
	pool, err := pgxpool.New(context.Background(), "postgres://valence@127.0.0.1:1/valence?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var logs syncBuffer
	q := New(pool)
	q.log = slog.New(slog.NewTextHandler(&logs, nil))
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q.Lease(ctx, []string{"test"}, time.Second) // starts the listener; fails too

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "queue listener failed") {
		if time.Now().After(deadline) {
			t.Fatalf("no listener error was logged; logs:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "connect") {
		t.Errorf("log doesn't say what failed:\n%s", logs.String())
	}
}
