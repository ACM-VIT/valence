package pgqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
	"github.com/ACM-VIT/valence/pkg/queue"
)

const (
	pollInterval  = 5 * time.Second
	listenBackoff = time.Second
)

// Queue is a queue.Queue backed by the jobs table.
//
// Idle Lease calls don't hold pool connections. One listener connection,
// opened outside the pool on the first Lease, waits for NOTIFY and wakes
// every idle Lease, so Enqueue, Heartbeat and Ack always find a free
// connection even with a one-connection pool.
type Queue struct {
	pool *pgxpool.Pool
	q    *db.Queries
	poll time.Duration
	log  *slog.Logger

	start sync.Once
	ctx   context.Context // cancelled by Close
	stop  context.CancelFunc
	done  chan struct{} // closed when the listener exits

	mu   sync.Mutex
	wake chan struct{} // closed and replaced on every notification
}

func New(pool *pgxpool.Pool) *Queue {
	ctx, stop := context.WithCancel(context.Background())
	return &Queue{
		pool: pool, q: db.New(pool), poll: pollInterval, log: slog.Default(),
		ctx: ctx, stop: stop, done: make(chan struct{}),
		wake: make(chan struct{}),
	}
}

var _ queue.Queue = (*Queue)(nil)

// Close stops the listener. It does not close the pool.
func (q *Queue) Close() {
	q.start.Do(func() { close(q.done) }) // never started: nothing to wait for
	q.stop()
	<-q.done
}

func (q *Queue) Enqueue(ctx context.Context, job queue.Job) (string, error) {
	if err := queue.CheckPriority(job.Priority); err != nil {
		return "", err
	}
	payload := job.Payload
	if payload == nil {
		payload = []byte{}
	}
	var dedupe *string
	if job.DedupeKey != "" {
		dedupe = &job.DedupeKey
	}
	id, err := q.q.EnqueueJob(ctx, db.EnqueueJobParams{
		Kind:      job.Kind,
		Priority:  int16(job.Priority),
		Payload:   payload,
		DedupeKey: dedupe,
	})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

func (q *Queue) Lease(ctx context.Context, kinds []string, ttl time.Duration) (queue.Lease, error) {
	q.start.Do(func() { go q.listen() })
	for {
		// Take the wake channel before looking, so a job inserted between
		// the look and the wait still wakes us.
		wake := q.wakeChan()
		row, err := q.q.LeaseJob(ctx, db.LeaseJobParams{TtlMs: ttl.Milliseconds(), Kinds: kinds})
		if err == nil {
			return toLease(row), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return queue.Lease{}, err
		}

		// The poll also covers nacked and expired jobs, which come back
		// without a NOTIFY, and any time the listener is reconnecting.
		timer := time.NewTimer(q.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return queue.Lease{}, ctx.Err()
		case <-wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (q *Queue) wakeChan() chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.wake
}

func (q *Queue) broadcast() {
	q.mu.Lock()
	defer q.mu.Unlock()
	close(q.wake)
	q.wake = make(chan struct{})
}

// listen keeps one LISTEN connection open until Close, reconnecting after
// errors. Failures are logged, since until it reconnects idle workers only
// find new jobs on the slower poll.
func (q *Queue) listen() {
	defer close(q.done)
	for {
		err := q.listenOnce()
		if q.ctx.Err() != nil {
			return
		}
		q.log.Warn("queue listener failed; idle workers fall back to polling until it reconnects",
			"err", err, "retry_in", listenBackoff, "poll", q.poll)
		select {
		case <-q.ctx.Done():
			return
		case <-time.After(listenBackoff):
		}
	}
}

func (q *Queue) listenOnce() error {
	conn, err := pgx.ConnectConfig(q.ctx, q.pool.Config().ConnConfig)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(q.ctx, "LISTEN jobs"); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}
	// Jobs may have arrived while we weren't listening.
	q.broadcast()
	for {
		if _, err := conn.WaitForNotification(q.ctx); err != nil {
			return fmt.Errorf("wait for notification: %w", err)
		}
		q.broadcast()
	}
}

func (q *Queue) Heartbeat(ctx context.Context, l queue.Lease, ttl time.Duration) (queue.Lease, error) {
	id, token, err := parse(l)
	if err != nil {
		return queue.Lease{}, err
	}
	until, err := q.q.HeartbeatJob(ctx, db.HeartbeatJobParams{TtlMs: ttl.Milliseconds(), ID: id, Token: token})
	if errors.Is(err, pgx.ErrNoRows) {
		return queue.Lease{}, queue.ErrLeaseLost
	}
	if err != nil {
		return queue.Lease{}, err
	}
	l.Deadline = *until
	return l, nil
}

func (q *Queue) Ack(ctx context.Context, l queue.Lease) error {
	id, token, err := parse(l)
	if err != nil {
		return err
	}
	n, err := q.q.AckJob(ctx, db.AckJobParams{ID: id, Token: token})
	if err != nil {
		return err
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (q *Queue) Nack(ctx context.Context, l queue.Lease, delay time.Duration) error {
	id, token, err := parse(l)
	if err != nil {
		return err
	}
	n, err := q.q.NackJob(ctx, db.NackJobParams{DelayMs: delay.Milliseconds(), ID: id, Token: token})
	if err != nil {
		return err
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func toLease(r db.LeaseJobRow) queue.Lease {
	var dedupe string
	if r.DedupeKey != nil {
		dedupe = *r.DedupeKey
	}
	return queue.Lease{
		Job: queue.Job{
			ID:        strconv.FormatInt(r.ID, 10),
			Kind:      r.Kind,
			Priority:  int(r.Priority),
			Payload:   r.Payload,
			DedupeKey: dedupe,
		},
		Attempt:  int(r.Attempts),
		Token:    r.LeaseToken.String(),
		Deadline: *r.LeasedUntil,
	}
}

func parse(l queue.Lease) (int64, pgtype.UUID, error) {
	id, err := strconv.ParseInt(l.Job.ID, 10, 64)
	if err != nil {
		return 0, pgtype.UUID{}, queue.ErrLeaseLost
	}
	var token pgtype.UUID
	if err := token.Scan(l.Token); err != nil || !token.Valid {
		return 0, pgtype.UUID{}, queue.ErrLeaseLost
	}
	return id, token, nil
}
