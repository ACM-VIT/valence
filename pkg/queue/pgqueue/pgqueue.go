package pgqueue

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
	"github.com/ACM-VIT/valence/pkg/queue"
)

const pollInterval = 5 * time.Second

type Queue struct {
	pool *pgxpool.Pool
	q    *db.Queries
	poll time.Duration
}

func New(pool *pgxpool.Pool) *Queue {
	return &Queue{pool: pool, q: db.New(pool), poll: pollInterval}
}

var _ queue.Queue = (*Queue)(nil)

func (q *Queue) Enqueue(ctx context.Context, job queue.Job) (string, error) {
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
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return queue.Lease{}, err
	}
	defer func() {
		conn.Exec(context.Background(), "UNLISTEN jobs")
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, "LISTEN jobs"); err != nil {
		return queue.Lease{}, err
	}

	lq := db.New(conn)
	for {
		row, err := lq.LeaseJob(ctx, db.LeaseJobParams{TtlMs: ttl.Milliseconds(), Kinds: kinds})
		if err == nil {
			return toLease(row), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return queue.Lease{}, err
		}

		wait, cancel := context.WithTimeout(ctx, q.poll)
		err = conn.Conn().PgConn().WaitForNotification(wait)
		cancel()
		if ctx.Err() != nil {
			return queue.Lease{}, ctx.Err()
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return queue.Lease{}, err
		}
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
	if err := token.Scan(l.Token); err != nil {
		return 0, pgtype.UUID{}, queue.ErrLeaseLost
	}
	return id, token, nil
}
