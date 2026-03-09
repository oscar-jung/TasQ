ALTER TABLE projects
ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'manual';

ALTER TABLE projects
DROP CONSTRAINT IF EXISTS chk_projects_execution_mode;

ALTER TABLE projects
ADD CONSTRAINT chk_projects_execution_mode
CHECK (execution_mode IN ('manual', 'agent_assisted', 'agent_autonomous'));

ALTER TABLE projects
ADD COLUMN IF NOT EXISTS plan_state TEXT NOT NULL DEFAULT 'draft';

ALTER TABLE projects
DROP CONSTRAINT IF EXISTS chk_projects_plan_state;

ALTER TABLE projects
ADD CONSTRAINT chk_projects_plan_state
CHECK (plan_state IN ('draft', 'approved', 'archived'));
