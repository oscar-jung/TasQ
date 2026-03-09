ALTER TABLE tasks
ADD COLUMN IF NOT EXISTS required_capabilities TEXT[] NOT NULL DEFAULT '{}'::text[];

CREATE INDEX IF NOT EXISTS idx_tasks_required_capabilities_gin
ON tasks USING GIN (required_capabilities);
