// Package memqueue is an in-memory queue.Queue for tests and single-process
// development. Jobs are lost when the process exits.
package memqueue

import (
	"context"
	"crypto/rand"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ACM-VIT/valence/pkg/queue"
)

type job struct {
	queue.Job
	seq       int64
	available time.Time
	token     string
	until     time.Time
	attempts  int
}

type Queue struct {
	mu     sync.Mutex
	next   int64
	jobs   map[string]*job
	dedupe map[string]string // dedupe key → job ID
	wake   chan struct{}     // closed and replaced on every Enqueue
}

func New() *Queue {
	return &Queue{jobs: map[string]*job{}, dedupe: map[string]string{}, wake: make(chan struct{})}
}

var _ queue.Queue = (*Queue)(nil)

func (q *Queue) Enqueue(_ context.Context, j queue.Job) (string, error) {
	if err := queue.CheckPriority(j.Priority); err != nil {
		return "", err
	}
	// Copy, so the caller can reuse its buffer.
	j.Payload = slices.Clone(j.Payload)
	q.mu.Lock()
	defer q.mu.Unlock()
	if id, ok := q.dedupe[j.DedupeKey]; ok && j.DedupeKey != "" {
		return id, nil
	}
	q.next++
	j.ID = strconv.FormatInt(q.next, 10)
	q.jobs[j.ID] = &job{Job: j, seq: q.next, available: time.Now()}
	if j.DedupeKey != "" {
		q.dedupe[j.DedupeKey] = j.ID
	}
	close(q.wake)
	q.wake = make(chan struct{})
	return j.ID, nil
}

func (q *Queue) Lease(ctx context.Context, kinds []string, ttl time.Duration) (queue.Lease, error) {
	for {
		l, ok, wake := q.tryLease(kinds, ttl)
		if ok {
			return l, nil
		}
		// Short poll as well as the wake channel: nacked and expired jobs
		// come back without an Enqueue.
		select {
		case <-ctx.Done():
			return queue.Lease{}, ctx.Err()
		case <-wake:
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (q *Queue) tryLease(kinds []string, ttl time.Duration) (queue.Lease, bool, chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	var best *job
	for _, j := range q.jobs {
		if !slices.Contains(kinds, j.Kind) || j.available.After(now) || j.until.After(now) {
			continue
		}
		if best == nil || j.Priority > best.Priority || (j.Priority == best.Priority && j.seq < best.seq) {
			best = j
		}
	}
	if best == nil {
		return queue.Lease{}, false, q.wake
	}
	best.attempts++
	best.token = rand.Text()
	best.until = now.Add(ttl)
	// Copy, so a worker changing its payload can't change what a retry gets.
	j := best.Job
	j.Payload = slices.Clone(j.Payload)
	return queue.Lease{Job: j, Attempt: best.attempts, Token: best.token, Deadline: best.until}, true, nil
}

// owned returns the job if l is still its current lease. Call with q.mu held.
// An unleased job has an empty token, so an empty l.Token never matches.
func (q *Queue) owned(l queue.Lease) (*job, error) {
	j, ok := q.jobs[l.Job.ID]
	if !ok || l.Token == "" || j.token != l.Token {
		return nil, queue.ErrLeaseLost
	}
	return j, nil
}

func (q *Queue) Heartbeat(_ context.Context, l queue.Lease, ttl time.Duration) (queue.Lease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, err := q.owned(l)
	if err != nil {
		return queue.Lease{}, err
	}
	j.until = time.Now().Add(ttl)
	l.Deadline = j.until
	return l, nil
}

func (q *Queue) Ack(_ context.Context, l queue.Lease) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, err := q.owned(l)
	if err != nil {
		return err
	}
	delete(q.jobs, j.ID)
	if j.DedupeKey != "" {
		delete(q.dedupe, j.DedupeKey)
	}
	return nil
}

func (q *Queue) Nack(_ context.Context, l queue.Lease, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, err := q.owned(l)
	if err != nil {
		return err
	}
	j.token, j.until = "", time.Time{}
	j.available = time.Now().Add(delay)
	return nil
}
