package queue

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrLeaseLost   = errors.New("queue: lease lost")
	ErrBadPriority = errors.New("queue: priority out of range")
)

// Priorities fit in a Postgres smallint, so every queue orders them the same.
const (
	MinPriority = math.MinInt16
	MaxPriority = math.MaxInt16
)

// CheckPriority returns ErrBadPriority if p is outside [MinPriority, MaxPriority].
func CheckPriority(p int) error {
	if p < MinPriority || p > MaxPriority {
		return fmt.Errorf("%w: %d", ErrBadPriority, p)
	}
	return nil
}

type Job struct {
	ID        string
	Kind      string
	Priority  int
	Payload   []byte
	DedupeKey string
}

type Lease struct {
	Job      Job
	Attempt  int
	Token    string
	Deadline time.Time
}

type Queue interface {
	Enqueue(ctx context.Context, job Job) (string, error)
	Lease(ctx context.Context, kinds []string, ttl time.Duration) (Lease, error)
	Heartbeat(ctx context.Context, l Lease, ttl time.Duration) (Lease, error)
	Ack(ctx context.Context, l Lease) error
	Nack(ctx context.Context, l Lease, delay time.Duration) error
}
