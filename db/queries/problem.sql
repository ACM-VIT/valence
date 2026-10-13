-- name: GetProblemBySlug :one
SELECT * FROM problems
WHERE slug = $1;

-- name: ListProblems :many
SELECT id, slug, title, visibility
FROM problems
WHERE (visibility = 'public' or sqlc.arg(include_private)::boolean)
    AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: ListTests :many
SELECT * FROM tests
WHERE problem_id = $1 
ORDER BY ordinal;

-- name: ListSampleTests :many
SELECT * FROM tests
WHERE problem_id = $1 AND is_sample
ORDER BY ordinal;

-- name: LockProblem :one
-- Serialises writers of one problem's tests until the transaction ends.
SELECT id FROM problems WHERE id = $1 FOR UPDATE;

-- name: DeleteTests :exec
DELETE FROM tests
WHERE problem_id =  $1;

-- name: InsertTests :copyfrom
INSERT INTO tests (problem_id, ordinal, input_hash, input_size, output_hash, output_size, is_sample)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: CreateProblem :one
-- Returns no row if the slug already exists (for example, created by a
-- concurrent import); the caller then updates that problem instead.
INSERT INTO problems (slug, title, statement_md, time_limit_ms, memory_limit_kib, checker)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (slug) DO NOTHING
RETURNING id;

-- name: UpdateProblem :one
UPDATE problems
SET title = $2, statement_md = $3, time_limit_ms = $4, memory_limit_kib = $5, checker = $6,
    revision = revision + 1
WHERE id = $1
RETURNING revision;
