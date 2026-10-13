package submissions

import "github.com/ACM-VIT/valence/gen/go/db"

var forward = map[db.SubmissionStatus]db.SubmissionStatus{
	db.SubmissionStatusLeased:    db.SubmissionStatusCompiling,
	db.SubmissionStatusCompiling: db.SubmissionStatusRunning,
	db.SubmissionStatusRunning:   db.SubmissionStatusChecking,
	db.SubmissionStatusChecking:  db.SubmissionStatusRunning,
}

func CanTransition(from, to db.SubmissionStatus) bool {
	if from == db.SubmissionStatusFinalized || from == to {
		return false
	}
	switch to {
	case db.SubmissionStatusLeased:
		return true
	case db.SubmissionStatusRetryableError, db.SubmissionStatusFinalized:
		return from != db.SubmissionStatusQueued
	}
	return forward[from] == to
}
