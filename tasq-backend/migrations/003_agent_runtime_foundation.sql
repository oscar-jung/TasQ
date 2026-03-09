ALTER TABLE tasks
DROP CONSTRAINT IF EXISTS tasks_status_check;

ALTER TABLE tasks
DROP CONSTRAINT IF EXISTS chk_tasks_status;

ALTER TABLE tasks
ADD CONSTRAINT chk_tasks_status
CHECK (status IN ('planned', 'in_progress', 'done', 'failed'));

ALTER TABLE tasks
ADD COLUMN IF NOT EXISTS max_attempts INT NOT NULL DEFAULT 5;

ALTER TABLE tasks
DROP CONSTRAINT IF EXISTS chk_tasks_max_attempts;

ALTER TABLE tasks
ADD CONSTRAINT chk_tasks_max_attempts
CHECK (max_attempts >= 1);

ALTER TABLE task_claims
ADD COLUMN IF NOT EXISTS attempt_no INT NOT NULL DEFAULT 1;

ALTER TABLE task_claims
ADD COLUMN IF NOT EXISTS claim_token TEXT;

ALTER TABLE task_claims
ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS task_runs (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    attempt_no INT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'released')),
    result_payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_task_runs_task_started
ON task_runs(task_id, started_at DESC);

CREATE TABLE IF NOT EXISTS task_git_refs (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    base_commit TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_git_refs_task_created
ON task_git_refs(task_id, created_at DESC);

ALTER TABLE task_events
ADD COLUMN IF NOT EXISTS project_id BIGINT NULL REFERENCES projects(id) ON DELETE CASCADE;

UPDATE task_events e
SET project_id = t.project_id
FROM tasks t
WHERE e.task_id = t.id
  AND e.project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_task_events_project_created
ON task_events(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tasks_project_status_parent_order
ON tasks(project_id, status, parent_task_id, display_order);
