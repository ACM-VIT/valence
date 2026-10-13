// Package queuetest is a conformance suite every queue.Queue must pass.
package queuetest

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ACM-VIT/valence/pkg/queue"
)

// Run runs the suite. Each test uses its own job kind, so the queue may be
// shared between tests and hold other jobs.
func Run(t *testing.T, q queue.Queue) {
	t.Run("EnqueueLeaseAck", func(t *testing.T) { testEnqueueLeaseAck(t, q) })
	t.Run("NoDoubleDelivery", func(t *testing.T) { testNoDoubleDelivery(t, q) })
	t.Run("ExpiredLeaseRedelivered", func(t *testing.T) { testExpiredLease(t, q) })
	t.Run("HeartbeatKeepsLease", func(t *testing.T) { testHeartbeat(t, q) })
	t.Run("Priorities", func(t *testing.T) { testPriorities(t, q) })
	t.Run("NackDelays", func(t *testing.T) { testNack(t, q) })
	t.Run("Dedupe", func(t *testing.T) { testDedupe(t, q) })
	t.Run("OnlyRequestedKinds", func(t *testing.T) { testKinds(t, q) })
	t.Run("WakesOnEnqueue", func(t *testing.T) { testWakes(t, q) })
}

func kind() string { return "test-" + rand.Text()[:8] }

func enqueue(t *testing.T, q queue.Queue, j queue.Job) string {
	t.Helper()
	id, err := q.Enqueue(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func lease(t *testing.T, q queue.Queue, k string, ttl time.Duration) queue.Lease {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	l, err := q.Lease(ctx, []string{k}, ttl)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	return l
}

// empty checks that nothing of kind k can be leased right now.
func empty(t *testing.T, q queue.Queue, k string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if l, err := q.Lease(ctx, []string{k}, time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("leased %+v (err %v), want nothing available", l.Job, err)
	}
}

func testEnqueueLeaseAck(t *testing.T, q queue.Queue) {
	k := kind()
	id := enqueue(t, q, queue.Job{Kind: k, Payload: []byte("hello")})
	l := lease(t, q, k, time.Minute)
	if l.Job.ID != id || l.Job.Kind != k || string(l.Job.Payload) != "hello" || l.Attempt != 1 {
		t.Fatalf("got %+v attempt %d, want job %s with payload hello, attempt 1", l.Job, l.Attempt, id)
	}
	if l.Token == "" || time.Until(l.Deadline) <= 0 {
		t.Fatalf("lease has no token or deadline: %+v", l)
	}
	if err := q.Ack(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	empty(t, q, k)
	if err := q.Ack(context.Background(), l); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("second ack: %v, want ErrLeaseLost", err)
	}
}

func testNoDoubleDelivery(t *testing.T, q queue.Queue) {
	const jobs, workers = 100, 8
	k := kind()
	for range jobs {
		enqueue(t, q, queue.Job{Kind: k})
	}

	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				l, err := q.Lease(ctx, []string{k}, time.Minute)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					return // queue drained
				}
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				seen[l.Job.ID]++
				mu.Unlock()
				if err := q.Ack(context.Background(), l); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()

	if len(seen) != jobs {
		t.Errorf("delivered %d distinct jobs, want %d", len(seen), jobs)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s delivered %d times", id, n)
		}
	}
}

func testExpiredLease(t *testing.T, q queue.Queue) {
	ctx := context.Background()
	k := kind()
	enqueue(t, q, queue.Job{Kind: k})
	old := lease(t, q, k, 100*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	l := lease(t, q, k, time.Minute)
	if l.Job.ID != old.Job.ID || l.Attempt != 2 || l.Token == old.Token {
		t.Fatalf("got job %s attempt %d, want job %s again, attempt 2, new token", l.Job.ID, l.Attempt, old.Job.ID)
	}
	// The first worker woke up late: it no longer owns the job.
	if _, err := q.Heartbeat(ctx, old, time.Minute); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("heartbeat on old lease: %v, want ErrLeaseLost", err)
	}
	if err := q.Ack(ctx, old); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("ack on old lease: %v, want ErrLeaseLost", err)
	}
	if err := q.Nack(ctx, old, 0); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("nack on old lease: %v, want ErrLeaseLost", err)
	}
	if err := q.Ack(ctx, l); err != nil {
		t.Fatal(err)
	}
}

func testHeartbeat(t *testing.T, q queue.Queue) {
	k := kind()
	enqueue(t, q, queue.Job{Kind: k})
	l := lease(t, q, k, 300*time.Millisecond)
	first := l.Deadline
	for range 6 {
		time.Sleep(100 * time.Millisecond)
		var err error
		if l, err = q.Heartbeat(context.Background(), l, 300*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	// 600ms have passed, twice the TTL, but the job is still ours.
	empty(t, q, k)
	if !l.Deadline.After(first) {
		t.Errorf("deadline %v not after %v", l.Deadline, first)
	}
	if err := q.Ack(context.Background(), l); err != nil {
		t.Fatal(err)
	}
}

func testPriorities(t *testing.T, q queue.Queue) {
	k := kind()
	low := enqueue(t, q, queue.Job{Kind: k, Priority: 0})
	high1 := enqueue(t, q, queue.Job{Kind: k, Priority: 10})
	mid := enqueue(t, q, queue.Job{Kind: k, Priority: 5})
	high2 := enqueue(t, q, queue.Job{Kind: k, Priority: 10})

	// Highest priority first; oldest first within a priority.
	for _, want := range []string{high1, high2, mid, low} {
		l := lease(t, q, k, time.Minute)
		if l.Job.ID != want {
			t.Fatalf("leased %s (priority %d), want %s", l.Job.ID, l.Job.Priority, want)
		}
		if err := q.Ack(context.Background(), l); err != nil {
			t.Fatal(err)
		}
	}
}

func testNack(t *testing.T, q queue.Queue) {
	k := kind()
	enqueue(t, q, queue.Job{Kind: k})
	l := lease(t, q, k, time.Minute)
	if err := q.Nack(context.Background(), l, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	empty(t, q, k) // still inside the delay
	time.Sleep(300 * time.Millisecond)
	again := lease(t, q, k, time.Minute)
	if again.Job.ID != l.Job.ID || again.Attempt != 2 {
		t.Fatalf("got job %s attempt %d, want job %s attempt 2", again.Job.ID, again.Attempt, l.Job.ID)
	}
	if err := q.Ack(context.Background(), again); err != nil {
		t.Fatal(err)
	}
}

func testDedupe(t *testing.T, q queue.Queue) {
	k := kind()
	key := k + ":1"
	a := enqueue(t, q, queue.Job{Kind: k, DedupeKey: key})
	b := enqueue(t, q, queue.Job{Kind: k, DedupeKey: key})
	if a != b {
		t.Fatalf("same dedupe key gave jobs %s and %s", a, b)
	}
	l := lease(t, q, k, time.Minute)
	if l.Job.DedupeKey != key {
		t.Errorf("dedupe key %q, want %q", l.Job.DedupeKey, key)
	}
	empty(t, q, k)
	if err := q.Ack(context.Background(), l); err != nil {
		t.Fatal(err)
	}
}

func testKinds(t *testing.T, q queue.Queue) {
	a, b := kind(), kind()
	enqueue(t, q, queue.Job{Kind: a})
	empty(t, q, b)
	l := lease(t, q, a, time.Minute)
	if err := q.Ack(context.Background(), l); err != nil {
		t.Fatal(err)
	}
}

func testWakes(t *testing.T, q queue.Queue) {
	k := kind()
	got := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		l, err := q.Lease(ctx, []string{k}, time.Minute)
		if err == nil {
			err = q.Ack(context.Background(), l)
		}
		got <- err
	}()
	time.Sleep(200 * time.Millisecond) // let the worker go idle
	start := time.Now()
	enqueue(t, q, queue.Job{Kind: k})
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	// Far less than any polling interval: the insert woke the worker.
	if d := time.Since(start); d > time.Second {
		t.Errorf("idle worker took %v to see a new job", d)
	}
}
