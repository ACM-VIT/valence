-- name: CreateSubmission :one
INSERT INTO submissions (user_id, problem_id, problem_revision, language, source)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: CreateAttempt :exec
INSERT INTO submission_attempts (submission_id, attempt)
VALUES ($1, $2);

-- name: GetSubmission :one
SELECT * FROM submissions WHERE id = $1;

-- name: GetAttempt :one
SELECT * FROM submission_attempts WHERE submission_id = $1 AND attempt = $2;

-- name: ListUserSubmissions :many
SELECT id, problem_id, language, status, verdict, time_ms, memory_kib, created_at
FROM submissions
WHERE user_id = sqlc.arg(user_id)
    AND (sqlc.narg(problem_id)::bigint IS NULL OR problem_id = sqlc.narg(problem_id))
    AND id < sqlc.arg(before_id)
ORDER BY id desc
LIMIT sqlc.arg(page_size);

-- name: ClaimAttempt :execrows
-- Moves the attempt to 'leased' for the worker holding lease_token. A claim
-- from an older queue lease than the one that last claimed it is refused.
UPDATE submission_attempts
SET status = 'leased', lease_token = sqlc.arg(lease_token)::text,
    lease_generation = sqlc.arg(lease_generation)::integer,
    started_at = COALESCE(started_at, now())
WHERE submission_id = sqlc.arg(submission_id)
    AND attempt = sqlc.arg(attempt)
    AND status = sqlc.arg(from_status)
    AND (lease_generation IS NULL OR lease_generation < sqlc.arg(lease_generation)::integer);

-- name: SetAttemptStatus :execrows
UPDATE submission_attempts
SET status = sqlc.arg(to_status)
WHERE submission_id = sqlc.arg(submission_id)
    AND attempt = sqlc.arg(attempt)
    AND status = sqlc.arg(from_status)
    AND lease_token = sqlc.arg(lease_token)::text;

-- name: SetSubmissionStatus :exec
UPDATE submissions
SET status = sqlc.arg(status), current_test = sqlc.narg(current_test)
WHERE id = sqlc.arg(id) AND attempt = sqlc.arg(attempt);

-- name: InsertTestResults :exec
INSERT INTO test_results (submission_id, attempt, ordinal, verdict, time_ms, memory_kib)
SELECT sqlc.arg(submission_id), sqlc.arg(attempt),
        unnest(sqlc.arg(ordinals)::integer[]),
        unnest(sqlc.arg(verdicts)::text[])::verdict,
        unnest(sqlc.arg(times_ms)::integer[]),
        unnest(sqlc.arg(memories_kib)::integer[])
ON CONFLICT DO NOTHING;

-- name: ListTestResults :many
SELECT * FROM test_results
WHERE submission_id = $1 AND attempt = $2
ORDER BY ordinal;

-- name: FinalizeAttempt :execrows
UPDATE submission_attempts
SET status = 'finalized', verdict = sqlc.arg(verdict)::verdict,
    time_ms = sqlc.narg(time_ms), memory_kib = sqlc.narg(memory_kib),
    compile_output = sqlc.narg(compile_output), finished_at = now()
WHERE submission_id = sqlc.arg(submission_id)
    AND attempt = sqlc.arg(attempt)
    AND status NOT IN ('queued', 'finalized')
    AND lease_token = sqlc.arg(lease_token)::text;

-- name: FinalizeSubmission :exec
UPDATE submissions
SET status = 'finalized', verdict = sqlc.arg(verdict)::verdict,
    time_ms = sqlc.narg(time_ms), memory_kib = sqlc.narg(memory_kib),
    current_test = NULL, judged_at = now()
WHERE id = sqlc.arg(id) AND attempt = sqlc.arg(attempt);
