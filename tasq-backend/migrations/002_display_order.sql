ALTER TABLE tasks
ADD COLUMN IF NOT EXISTS display_order BIGINT;

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY project_id, parent_task_id
               ORDER BY created_at ASC, id ASC
           ) AS rn
    FROM tasks
)
UPDATE tasks t
SET display_order = ranked.rn * 1024
FROM ranked
WHERE t.id = ranked.id
  AND t.display_order IS NULL;

ALTER TABLE tasks
ALTER COLUMN display_order SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_project_parent_order
ON tasks(project_id, parent_task_id, display_order);
