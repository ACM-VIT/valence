package submissions

import (
	"testing"

	"github.com/ACM-VIT/valence/gen/go/db"
)

func TestCanTransition(t *testing.T) {
	const (
		queued    = db.SubmissionStatusQueued
		leased    = db.SubmissionStatusLeased
		compiling = db.SubmissionStatusCompiling
		running   = db.SubmissionStatusRunning
		checking  = db.SubmissionStatusChecking
		finalized = db.SubmissionStatusFinalized
		retry     = db.SubmissionStatusRetryableError
	)
	tests := []struct {
		from, to db.SubmissionStatus
		want     bool
	}{
		// The normal path, one test after another.
		{queued, leased, true},
		{leased, compiling, true},
		{compiling, running, true},
		{running, checking, true},
		{checking, running, true},
		{checking, finalized, true},
		{compiling, finalized, true}, // compile error

		// Crashes and infrastructure errors.
		{running, leased, true},
		{running, retry, true},
		{retry, leased, true},
		{retry, finalized, true}, // gave up: IE

		// Not allowed.
		{queued, running, false},
		{queued, finalized, false},
		{queued, retry, false},
		{compiling, checking, false},
		{running, queued, false},
		{finalized, leased, false},
		{finalized, running, false},
		{running, running, false},
	}
	for _, tt := range tests {
		if got := CanTransition(tt.from, tt.to); got != tt.want {
			t.Errorf("%s → %s = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}
