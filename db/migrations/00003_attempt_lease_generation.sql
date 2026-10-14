-- +goose Up
-- The queue lease's attempt number for the worker that claimed this attempt.
-- It only grows, so a delayed claim from an expired lease (a lower number)
-- can't take the attempt back from the worker that replaced it.
ALTER TABLE submission_attempts ADD COLUMN lease_generation integer;

-- +goose Down
ALTER TABLE submission_attempts DROP COLUMN lease_generation;
