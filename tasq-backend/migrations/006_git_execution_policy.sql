ALTER TABLE projects
ADD COLUMN IF NOT EXISTS git_policy TEXT NOT NULL DEFAULT 'optional';

ALTER TABLE projects
DROP CONSTRAINT IF EXISTS chk_projects_git_policy;

ALTER TABLE projects
ADD CONSTRAINT chk_projects_git_policy
CHECK (git_policy IN ('optional', 'required'));

ALTER TABLE tasks
ADD COLUMN IF NOT EXISTS git_policy TEXT NOT NULL DEFAULT 'inherit';

ALTER TABLE tasks
DROP CONSTRAINT IF EXISTS chk_tasks_git_policy;

ALTER TABLE tasks
ADD CONSTRAINT chk_tasks_git_policy
CHECK (git_policy IN ('inherit', 'required', 'not_required'));
