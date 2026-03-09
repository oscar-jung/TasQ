package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	pq "github.com/lib/pq"
)

// createTask handles POST /projects/:project_id/tasks.
func (s *server) createTask(w http.ResponseWriter, r *http.Request, projectID int64) {
	var req createTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if req.MaxAttempts != nil && *req.MaxAttempts < 1 {
		http.Error(w, "max_attempts must be >= 1", http.StatusBadRequest)
		return
	}
	requiredCapabilities := normalizeCapabilities(req.RequiredCapabilities)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if err := lockProjectTopology(r.Context(), tx, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.ParentTaskID != nil {
		var count int
		err = tx.QueryRowContext(r.Context(), `
			SELECT COUNT(1) FROM tasks
			WHERE id = $1 AND project_id = $2`, *req.ParentTaskID, projectID).Scan(&count)
		if err != nil || count == 0 {
			http.Error(w, "parent task not found in project", http.StatusBadRequest)
			return
		}
	}

	var t task
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO tasks(project_id, parent_task_id, title, spec_md, status, max_attempts, required_capabilities, display_order)
		VALUES ($1, $2, $3, $4, 'planned', COALESCE($5, 5), $6::text[], COALESCE((
			SELECT MAX(display_order) + 1024
			FROM tasks
			WHERE project_id = $1
			  AND parent_task_id IS NOT DISTINCT FROM $2
		), 1024))
		RETURNING id, project_id, parent_task_id, title, spec_md, result_md, status, max_attempts, display_order, created_at, started_at, done_at`,
		projectID, req.ParentTaskID, req.Title, req.SpecMD, req.MaxAttempts, pq.Array(requiredCapabilities),
	).Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.MaxAttempts, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, t.ID, "task.created", actorType, actorID, map[string]any{
		"parent_task_id":        req.ParentTaskID,
		"title":                 t.Title,
		"max_attempts":          t.MaxAttempts,
		"required_capabilities": requiredCapabilities,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)
	writeJSON(w, http.StatusCreated, t)
}

// updateTaskCapabilities handles PATCH /tasks/:task_id/capabilities.
func (s *server) updateTaskCapabilities(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req updateTaskCapabilitiesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	requiredCapabilities := normalizeCapabilities(req.RequiredCapabilities)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET required_capabilities = $2::text[],
			updated_at = NOW()
		WHERE id = $1`, taskID, pq.Array(requiredCapabilities)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.capabilities.updated", actorType, actorID, map[string]any{
		"required_capabilities": requiredCapabilities,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// getTaskCapabilities handles GET /tasks/:task_id/capabilities.
func (s *server) getTaskCapabilities(w http.ResponseWriter, r *http.Request, taskID int64) {
	var capabilities []string
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT required_capabilities
		FROM tasks
		WHERE id = $1`, taskID).Scan(pq.Array(&capabilities)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":               taskID,
		"required_capabilities": capabilities,
	})
}

// addDependency handles POST /tasks/:task_id/dependencies.
func (s *server) addDependency(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req addDependencyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.PredecessorTaskID == taskID {
		http.Error(w, "self dependency is not allowed", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var successorProject int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&successorProject); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	var predecessorProject int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, req.PredecessorTaskID).Scan(&predecessorProject); err != nil {
		http.Error(w, "predecessor task not found", http.StatusNotFound)
		return
	}
	if predecessorProject != successorProject {
		http.Error(w, "cross-project dependency is not allowed", http.StatusBadRequest)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, successorProject); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cycle, err := wouldCreateCycle(r.Context(), tx, req.PredecessorTaskID, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cycle {
		http.Error(w, "dependency would create cycle", http.StatusBadRequest)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO task_dependencies(project_id, predecessor_task_id, successor_task_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (predecessor_task_id, successor_task_id) DO NOTHING`,
		successorProject, req.PredecessorTaskID, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// listDependencies handles GET /tasks/:task_id/dependencies.
func (s *server) listDependencies(w http.ResponseWriter, r *http.Request, taskID int64) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT p.id, p.title, p.status, 'predecessor'
		FROM task_dependencies td
		JOIN tasks p ON p.id = td.predecessor_task_id
		WHERE td.successor_task_id = $1
		UNION ALL
		SELECT s.id, s.title, s.status, 'successor'
		FROM task_dependencies td
		JOIN tasks s ON s.id = td.successor_task_id
		WHERE td.predecessor_task_id = $1
		ORDER BY 4, 1`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	deps := make([]taskDependency, 0)
	for rows.Next() {
		var d taskDependency
		if err := rows.Scan(&d.TaskID, &d.Title, &d.Status, &d.Direction); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		deps = append(deps, d)
	}
	writeJSON(w, http.StatusOK, deps)
}

// removeDependency handles DELETE /tasks/:task_id/dependencies/:predecessor_id.
func (s *server) removeDependency(w http.ResponseWriter, r *http.Request, taskID, predecessorID int64) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var successorProject int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&successorProject); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	var predecessorProject int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, predecessorID).Scan(&predecessorProject); err != nil {
		http.Error(w, "predecessor task not found", http.StatusNotFound)
		return
	}
	if predecessorProject != successorProject {
		http.Error(w, "cross-project dependency is not allowed", http.StatusBadRequest)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, successorProject); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		DELETE FROM task_dependencies
		WHERE predecessor_task_id = $1 AND successor_task_id = $2`, predecessorID, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": affected})
}

// wouldCreateCycle checks whether a dependency edge introduces a cycle.
func wouldCreateCycle(ctx context.Context, tx *sql.Tx, predecessorID, successorID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE reach(task_id) AS (
			SELECT successor_task_id FROM task_dependencies WHERE predecessor_task_id = $1
			UNION
			SELECT td.successor_task_id
			FROM task_dependencies td
			JOIN reach r ON td.predecessor_task_id = r.task_id
		)
		SELECT COUNT(1)
		FROM reach
		WHERE task_id = $2`, successorID, predecessorID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// updateTaskStatus handles PATCH /tasks/:task_id/status.
func (s *server) updateTaskStatus(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req updateTaskStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	status := strings.TrimSpace(req.Status)
	if status != "planned" && status != "in_progress" && status != "done" && status != "failed" {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.QueryRowContext(r.Context(), `SELECT status FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if status == "in_progress" || status == "done" {
		ready, err := areDependenciesDone(r.Context(), tx, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ready {
			http.Error(w, "all predecessor tasks must be done", http.StatusConflict)
			return
		}
	}

	query := `
		UPDATE tasks
		SET status = $2,
			result_md = CASE WHEN $3 <> '' THEN $3 ELSE result_md END,
			started_at = CASE WHEN $2 = 'in_progress' AND started_at IS NULL THEN NOW() ELSE started_at END,
			done_at = CASE WHEN $2 = 'done' THEN NOW() ELSE done_at END,
			updated_at = NOW()
		WHERE id = $1`
	if _, err := tx.ExecContext(r.Context(), query, taskID, status, req.ResultMD); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.status.updated", actorType, actorID, map[string]any{
		"from": currentStatus,
		"to":   status,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	projectID, pidErr := s.fetchProjectIDByTaskID(r.Context(), taskID)
	if pidErr == nil {
		s.maybeRunTreeGuard(r.Context(), projectID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// updateTaskExecutionPolicy handles PATCH /tasks/:task_id/execution-policy.
func (s *server) updateTaskExecutionPolicy(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req updateTaskExecutionPolicyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.MaxAttempts < 1 {
		http.Error(w, "max_attempts must be >= 1", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET max_attempts = $2,
			updated_at = NOW()
		WHERE id = $1`, taskID, req.MaxAttempts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.execution_policy.updated", actorType, actorID, map[string]any{
		"max_attempts": req.MaxAttempts,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// linkTaskGitRef handles POST /tasks/:task_id/git-link.
func (s *server) linkTaskGitRef(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req linkTaskGitRefReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Repo) == "" ||
		strings.TrimSpace(req.Branch) == "" ||
		strings.TrimSpace(req.BaseCommit) == "" ||
		strings.TrimSpace(req.CommitSHA) == "" {
		http.Error(w, "repo, branch, base_commit, commit_sha are required", http.StatusBadRequest)
		return
	}

	var projectID int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO task_git_refs(task_id, repo, branch, base_commit, commit_sha)
		VALUES ($1, $2, $3, $4, $5)`,
		taskID, req.Repo, req.Branch, req.BaseCommit, req.CommitSHA); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.git.linked", actorType, actorID, map[string]any{
		"repo":        req.Repo,
		"branch":      req.Branch,
		"base_commit": req.BaseCommit,
		"commit_sha":  req.CommitSHA,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// updateTaskContent handles PATCH /tasks/:task_id/content.
func (s *server) updateTaskContent(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req updateTaskContentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRowContext(r.Context(), `
		SELECT project_id
		FROM tasks
		WHERE id = $1
		FOR UPDATE`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET title = CASE WHEN $2::text IS NULL THEN title ELSE $2::text END,
			spec_md = CASE WHEN $3::text IS NULL THEN spec_md ELSE $3::text END,
			result_md = CASE WHEN $4::text IS NULL THEN result_md ELSE $4::text END,
			updated_at = NOW()
		WHERE id = $1`,
		taskID, req.Title, req.SpecMD, req.ResultMD); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.content.updated", actorType, actorID, map[string]any{
		"title_changed":  req.Title != nil,
		"spec_changed":   req.SpecMD != nil,
		"result_changed": req.ResultMD != nil,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteTask handles POST /tasks/:task_id/delete with strategy options.
func (s *server) deleteTask(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req deleteTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Strategy == "" {
		req.Strategy = "delete_subtree"
	}
	if req.Strategy != "delete_subtree" && req.Strategy != "promote_children" && req.Strategy != "delete_if_leaf" {
		http.Error(w, "invalid strategy", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var projectID int64
	var parentID *int64
	if err := tx.QueryRowContext(r.Context(), `
		SELECT project_id, parent_task_id
		FROM tasks
		WHERE id = $1
		FOR UPDATE`, taskID).Scan(&projectID, &parentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch req.Strategy {
	case "delete_subtree":
		if _, err := tx.ExecContext(r.Context(), `
			WITH RECURSIVE subtree AS (
				SELECT id FROM tasks WHERE id = $1
				UNION ALL
				SELECT t.id
				FROM tasks t
				JOIN subtree s ON t.parent_task_id = s.id
			)
			DELETE FROM tasks
			WHERE id IN (SELECT id FROM subtree)`, taskID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "delete_if_leaf":
		var childCount int
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(1) FROM tasks WHERE parent_task_id = $1`, taskID).Scan(&childCount); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if childCount > 0 {
			http.Error(w, "task has child tasks", http.StatusConflict)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM tasks WHERE id = $1`, taskID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "promote_children":
		rows, err := tx.QueryContext(r.Context(), `
			SELECT id
			FROM tasks
			WHERE parent_task_id = $1
			ORDER BY display_order ASC, created_at ASC
			FOR UPDATE`, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		childIDs := make([]int64, 0)
		for rows.Next() {
			var childID int64
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			childIDs = append(childIDs, childID)
		}
		rows.Close()

		var orderBase int64
		if err := tx.QueryRowContext(r.Context(), `
			SELECT COALESCE(MAX(display_order), 0)
			FROM tasks
			WHERE project_id = $1
			  AND parent_task_id IS NOT DISTINCT FROM $2
			  AND id <> $3`, projectID, parentID, taskID).Scan(&orderBase); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, childID := range childIDs {
			if _, err := tx.ExecContext(r.Context(), `
				UPDATE tasks
				SET parent_task_id = $2,
					display_order = $3,
					updated_at = NOW()
				WHERE id = $1`, childID, parentID, orderBase+int64((i+1)*1024)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if _, err := tx.ExecContext(r.Context(), `DELETE FROM tasks WHERE id = $1`, taskID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	deletedTaskID := taskID
	s.streamBroker.publish(projectSignal{
		ProjectID: projectID,
		TaskID:    &deletedTaskID,
		EventType: "task.deleted",
		CreatedAt: time.Now().UTC(),
		Payload: map[string]any{
			"strategy":       req.Strategy,
			"parent_task_id": parentID,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "strategy": req.Strategy})
}

// areDependenciesDone validates status transition preconditions.
func areDependenciesDone(ctx context.Context, tx *sql.Tx, taskID int64) (bool, error) {
	var missing int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM task_dependencies td
		JOIN tasks t ON t.id = td.predecessor_task_id
		WHERE td.successor_task_id = $1
		  AND t.status <> 'done'`, taskID).Scan(&missing)
	if err != nil {
		return false, err
	}
	return missing == 0, nil
}

// reorderTasks handles POST /tasks/reorder for sibling ordering.
func (s *server) reorderTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req reorderTasksReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ProjectID == 0 {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	if !s.requireProjectAccess(w, r, req.ProjectID, "task:admin", true) {
		return
	}
	if len(req.OrderedTask) == 0 {
		http.Error(w, "ordered_task_ids is required", http.StatusBadRequest)
		return
	}

	seen := make(map[int64]struct{}, len(req.OrderedTask))
	for _, id := range req.OrderedTask {
		if _, ok := seen[id]; ok {
			http.Error(w, "ordered_task_ids must be unique", http.StatusBadRequest)
			return
		}
		seen[id] = struct{}{}
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if err := lockProjectTopology(r.Context(), tx, req.ProjectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := tx.QueryContext(r.Context(), `
		SELECT id, project_id, parent_task_id
		FROM tasks
		WHERE id = ANY($1)
		FOR UPDATE`, pq.Array(req.OrderedTask))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	found := make(map[int64]struct{}, len(req.OrderedTask))
	for rows.Next() {
		var id, projectID int64
		var parentID *int64
		if err := rows.Scan(&id, &projectID, &parentID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if projectID != req.ProjectID {
			http.Error(w, "all tasks must belong to the same project", http.StatusBadRequest)
			return
		}
		if !nullableTaskIDEqual(parentID, req.ParentTaskID) {
			http.Error(w, "all tasks must share the same parent_task_id", http.StatusBadRequest)
			return
		}
		found[id] = struct{}{}
	}

	if len(found) != len(req.OrderedTask) {
		http.Error(w, "some tasks not found", http.StatusBadRequest)
		return
	}

	var siblingCount int
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COUNT(1)
		FROM tasks
		WHERE project_id = $1
		  AND parent_task_id IS NOT DISTINCT FROM $2`, req.ProjectID, req.ParentTaskID).Scan(&siblingCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if siblingCount != len(req.OrderedTask) {
		http.Error(w, "ordered_task_ids must include all siblings under parent", http.StatusBadRequest)
		return
	}

	if err := applySiblingOrder(r.Context(), tx, req.OrderedTask); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	actorType, actorID := actorForEvent(r)
	if len(req.OrderedTask) > 0 {
		if err := logEvent(r.Context(), tx, req.OrderedTask[0], "task.reordered", actorType, actorID, map[string]any{
			"project_id":       req.ProjectID,
			"parent_task_id":   req.ParentTaskID,
			"ordered_task_ids": req.OrderedTask,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), req.ProjectID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// moveTask handles POST /tasks/:task_id/move.
func (s *server) moveTask(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req moveTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var projectID int64
	if err := tx.QueryRowContext(r.Context(), `
		SELECT project_id
		FROM tasks
		WHERE id = $1
		FOR UPDATE`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := lockProjectTopology(r.Context(), tx, projectID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.NewParentTaskID != nil {
		if *req.NewParentTaskID == taskID {
			http.Error(w, "task cannot be its own parent", http.StatusBadRequest)
			return
		}
		var parentProjectID int64
		if err := tx.QueryRowContext(r.Context(), `
			SELECT project_id
			FROM tasks
			WHERE id = $1
			FOR UPDATE`, *req.NewParentTaskID).Scan(&parentProjectID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "new parent task not found", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if parentProjectID != projectID {
			http.Error(w, "new parent must be in the same project", http.StatusBadRequest)
			return
		}
		isDescendant, err := isDescendantTask(r.Context(), tx, taskID, *req.NewParentTaskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if isDescendant {
			http.Error(w, "cannot move task under its own subtree", http.StatusBadRequest)
			return
		}
	}

	if req.NewIndex == nil {
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE tasks
			SET parent_task_id = $2,
				display_order = COALESCE((
					SELECT MAX(display_order) + 1024
					FROM tasks
					WHERE project_id = $3
					  AND parent_task_id IS NOT DISTINCT FROM $2
					  AND id <> $1
				), 1024),
				updated_at = NOW()
			WHERE id = $1`, taskID, req.NewParentTaskID, projectID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if *req.NewIndex < 0 {
			http.Error(w, "new_index must be >= 0", http.StatusBadRequest)
			return
		}
		siblingIDs, err := fetchSiblingIDsForUpdate(r.Context(), tx, projectID, req.NewParentTaskID, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		insertIndex := *req.NewIndex
		if insertIndex > len(siblingIDs) {
			insertIndex = len(siblingIDs)
		}
		ordered := make([]int64, 0, len(siblingIDs)+1)
		ordered = append(ordered, siblingIDs[:insertIndex]...)
		ordered = append(ordered, taskID)
		ordered = append(ordered, siblingIDs[insertIndex:]...)

		if _, err := tx.ExecContext(r.Context(), `
			UPDATE tasks
			SET parent_task_id = $2,
				updated_at = NOW()
			WHERE id = $1`, taskID, req.NewParentTaskID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := applySiblingOrder(r.Context(), tx, ordered); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	actorType, actorID := actorForEvent(r)
	if err := logEvent(r.Context(), tx, taskID, "task.moved", actorType, actorID, map[string]any{
		"new_parent_task_id": req.NewParentTaskID,
		"new_index":          req.NewIndex,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.maybeRunTreeGuard(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// logEvent writes audit events for task lifecycle actions.
func logEvent(ctx context.Context, tx *sql.Tx, taskID int64, eventType, actorType, actorID string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(task_id, project_id, event_type, actor_type, actor_id, payload_json)
		VALUES (
			$1,
			(SELECT project_id FROM tasks WHERE id = $1),
			$2,
			$3,
			$4,
			$5::jsonb
		)`,
		taskID, eventType, actorType, actorID, string(payloadJSON))
	return err
}

// applySiblingOrder rewrites display_order for a sibling sequence.
func applySiblingOrder(ctx context.Context, tx *sql.Tx, orderedTaskIDs []int64) error {
	for i, taskID := range orderedTaskIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET display_order = $2,
				updated_at = NOW()
			WHERE id = $1`, taskID, int64((i+1)*1024)); err != nil {
			return err
		}
	}
	return nil
}

// fetchSiblingIDsForUpdate returns sibling ids with row locks.
func fetchSiblingIDsForUpdate(ctx context.Context, tx *sql.Tx, projectID int64, parentID *int64, excludeTaskID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM tasks
		WHERE project_id = $1
		  AND parent_task_id IS NOT DISTINCT FROM $2
		  AND id <> $3
		ORDER BY display_order ASC, created_at ASC
		FOR UPDATE`, projectID, parentID, excludeTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// isDescendantTask checks whether candidate is inside root subtree.
func isDescendantTask(ctx context.Context, tx *sql.Tx, rootTaskID, candidateTaskID int64) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE descendants AS (
			SELECT id
			FROM tasks
			WHERE parent_task_id = $1
			UNION ALL
			SELECT t.id
			FROM tasks t
			JOIN descendants d ON t.parent_task_id = d.id
		)
		SELECT COUNT(1)
		FROM descendants
		WHERE id = $2`, rootTaskID, candidateTaskID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeCapabilities(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		clean := strings.ToLower(strings.TrimSpace(v))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// nullableTaskIDEqual compares nullable parent ids.
func nullableTaskIDEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// lockProjectTopology serializes topology updates by project.
func lockProjectTopology(ctx context.Context, tx *sql.Tx, projectID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock((71425::bigint << 32) + $1::bigint)`, projectID)
	return err
}
