-- name: FindByID :one
SELECT id, data, metadata, created_at
FROM jsonb_example
WHERE id = pggen.arg('id');

-- name: Create :one
INSERT INTO jsonb_example (data, metadata)
VALUES (pggen.arg('data'), pggen.arg('metadata'))
RETURNING id, data, metadata, created_at;

-- name: ListAll :many
SELECT id, data, metadata, created_at
FROM jsonb_example
ORDER BY created_at DESC;
