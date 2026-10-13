-- name: EnqueueJob :one
INSERT INTO jobs (kind, priority, payload, dedupe_key)
VALUES (sqlc.arg(kind), sqlc.arg(priority), sqlc.arg(payload), sqlc.narg(dedupe_key))

ON CONFLICT (dedupe_key) DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
RETURNING id;

-- name: LeaseJob :one
UPDATE jobs
SET lease_token = gen_random_uuid(),
    leased_until = now() + sqlc.arg(ttl_ms)::bigint * interval '1 millisecond',
    attempts = attempts + 1
WHERE id = (
    SELECT j.id FROM jobs j
    WHERE j.kind = ANY(sqlc.arg(kinds)::text[])
        AND j.available_at <= now()
        AND (j.leased_until IS NULL OR j.leased_until < now())
    ORDER BY j.priority DESC, j.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, kind, priority, payload, dedupe_key, attempts, lease_token, leased_until;


-- name: HeartbeatJob :one
UPDATE jobs
SET leased_until = now() + sqlc.arg(ttl_ms)::bigint * interval '1 millisecond'
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(token)
RETURNING leased_until;

-- name: AckJob :execrows
DELETE FROM jobs WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(token);

-- name: NackJob :execrows
UPDATE jobs
SET lease_token = NULL, leased_until = NULL,
    available_at = now() + sqlc.arg(delay_ms)::bigint * interval '1 millisecond'
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(token);
