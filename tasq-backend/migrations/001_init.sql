CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tasks (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_task_id BIGINT NULL REFERENCES tasks(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    spec_md TEXT NOT NULL DEFAULT '',
    result_md TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('planned', 'in_progress', 'done')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NULL,
    done_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_project_created ON tasks(project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_project_parent_created ON tasks(project_id, parent_task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_project_status ON tasks(project_id, status);

CREATE TABLE IF NOT EXISTS task_dependencies (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    predecessor_task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    successor_task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_task_dependency UNIQUE (predecessor_task_id, successor_task_id),
    CONSTRAINT chk_not_self_dependency CHECK (predecessor_task_id <> successor_task_id)
);

CREATE INDEX IF NOT EXISTS idx_task_dependencies_successor ON task_dependencies(successor_task_id);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_predecessor ON task_dependencies(predecessor_task_id);

CREATE TABLE IF NOT EXISTS task_claims (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    claimed_by_type TEXT NOT NULL CHECK (claimed_by_type IN ('human', 'agent')),
    claimed_by_id TEXT NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'released', 'expired')) DEFAULT 'active'
);

CREATE INDEX IF NOT EXISTS idx_task_claims_task_status_lease ON task_claims(task_id, status, lease_until);

CREATE TABLE IF NOT EXISTS task_events (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_events_task_created ON task_events(task_id, created_at);
