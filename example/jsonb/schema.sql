CREATE TABLE jsonb_example (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    data       jsonb NOT NULL DEFAULT '{}',
    metadata   json,
    created_at timestamptz NOT NULL DEFAULT now()
);
