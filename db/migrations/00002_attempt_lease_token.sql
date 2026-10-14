-- +goose Up
-- The queue lease token of the worker judging this attempt. A worker sets it
-- when it moves the attempt to 'leased', and every later write must carry it,
-- so a worker whose lease expired can't overwrite its replacement's work.
ALTER TABLE submission_attempts ADD COLUMN lease_token text;

-- +goose Down
ALTER TABLE submission_attempts DROP COLUMN lease_token;
