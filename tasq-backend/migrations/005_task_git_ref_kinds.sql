ALTER TABLE task_git_refs
ADD COLUMN IF NOT EXISTS ref_kind TEXT NOT NULL DEFAULT 'produced';

ALTER TABLE task_git_refs
DROP CONSTRAINT IF EXISTS chk_task_git_refs_ref_kind;

ALTER TABLE task_git_refs
ADD CONSTRAINT chk_task_git_refs_ref_kind
CHECK (ref_kind IN ('baseline', 'produced', 'rerun_branch'));

CREATE INDEX IF NOT EXISTS idx_task_git_refs_task_kind_created
ON task_git_refs(task_id, ref_kind, created_at DESC);
