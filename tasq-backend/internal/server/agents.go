package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// claimNext handles POST /agents/claim-next.
func (s *server) claimNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req claimNextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ProjectID == 0 || strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "project_id and agent_id are required", http.StatusBadRequest)
		return
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 300
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var t task
	err = tx.QueryRowContext(r.Context(), `
		SELECT t.id, t.project_id, t.parent_task_id, t.title, t.spec_md, t.result_md, t.status, t.display_order, t.created_at, t.started_at, t.done_at
		FROM tasks t
		WHERE t.project_id = $1
		  AND t.status = 'planned'
		  AND NOT EXISTS (
			SELECT 1 FROM task_claims c
			WHERE c.task_id = t.id
			  AND c.status = 'active'
			  AND c.lease_until > NOW()
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM task_dependencies td
			JOIN tasks p ON p.id = td.predecessor_task_id
			WHERE td.successor_task_id = t.id
			  AND p.status <> 'done'
		  )
		ORDER BY t.display_order ASC, t.created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, req.ProjectID).
		Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil, "message": "no claimable task"})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO task_claims(task_id, claimed_by_type, claimed_by_id, lease_until, status)
		VALUES ($1, 'agent', $2, NOW() + ($3::text || ' seconds')::interval, 'active')`,
		t.ID, req.AgentID, req.LeaseSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET status = 'in_progress',
			started_at = COALESCE(started_at, NOW()),
			updated_at = NOW()
		WHERE id = $1`, t.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, t.ID, "task.claimed", "agent", req.AgentID, map[string]any{
		"lease_seconds": req.LeaseSeconds,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"task": t})
}

// heartbeatTaskClaim handles POST /tasks/:task_id/heartbeat.
func (s *server) heartbeatTaskClaim(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req heartbeatReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 300
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claimID, err := getActiveClaimID(r.Context(), tx, taskID, req.AgentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "active claim not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE task_claims
		SET lease_until = NOW() + ($2::text || ' seconds')::interval
		WHERE id = $1`, claimID, req.LeaseSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.claim.heartbeat", "agent", req.AgentID, map[string]any{
		"lease_seconds": req.LeaseSeconds,
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

// releaseTaskClaim handles POST /tasks/:task_id/release.
func (s *server) releaseTaskClaim(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req releaseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}
	if req.ToStatus == "" {
		req.ToStatus = "planned"
	}
	if req.ToStatus != "planned" && req.ToStatus != "in_progress" {
		http.Error(w, "to_status must be planned or in_progress", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claimID, err := getActiveClaimID(r.Context(), tx, taskID, req.AgentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "active claim not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE task_claims
		SET status = 'released', released_at = NOW()
		WHERE id = $1`, claimID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET status = $2,
			updated_at = NOW()
		WHERE id = $1 AND status <> 'done'`, taskID, req.ToStatus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.claim.released", "agent", req.AgentID, map[string]any{
		"to_status": req.ToStatus,
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

// completeTask handles POST /tasks/:task_id/complete.
func (s *server) completeTask(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req completeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claimID, err := getActiveClaimID(r.Context(), tx, taskID, req.AgentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "active claim not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ready, err := areDependenciesDone(r.Context(), tx, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ready {
		http.Error(w, "all predecessor tasks must be done", http.StatusConflict)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET status = 'done',
			result_md = CASE WHEN $2 <> '' THEN $2 ELSE result_md END,
			done_at = NOW(),
			updated_at = NOW()
		WHERE id = $1`, taskID, req.ResultMD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE task_claims
		SET status = 'released', released_at = NOW()
		WHERE id = $1`, claimID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.completed", "agent", req.AgentID, map[string]any{}); err != nil {
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

// getActiveClaimID fetches the current active lease for an agent-task pair.
func getActiveClaimID(ctx context.Context, tx *sql.Tx, taskID int64, agentID string) (int64, error) {
	var claimID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM task_claims
		WHERE task_id = $1
		  AND claimed_by_id = $2
		  AND status = 'active'
		  AND lease_until > NOW()
		ORDER BY claimed_at DESC
		LIMIT 1
		FOR UPDATE`, taskID, agentID).Scan(&claimID)
	if err != nil {
		return 0, err
	}
	return claimID, nil
}

// getTaskContext handles GET /tasks/:task_id/context.
func (s *server) getTaskContext(w http.ResponseWriter, r *http.Request, taskID int64) {
	var ctxOut taskContext

	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, display_order, created_at, started_at, done_at
		FROM tasks
		WHERE id = $1`, taskID).
		Scan(&ctxOut.Task.ID, &ctxOut.Task.ProjectID, &ctxOut.Task.ParentID, &ctxOut.Task.Title, &ctxOut.Task.SpecMD, &ctxOut.Task.ResultMD, &ctxOut.Task.Status, &ctxOut.Task.DisplayOrder, &ctxOut.Task.CreatedAt, &ctxOut.Task.StartedAt, &ctxOut.Task.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	depRows, err := s.db.QueryContext(r.Context(), `
		SELECT p.id, p.title, p.result_md, COALESCE(to_char(p.done_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		FROM task_dependencies td
		JOIN tasks p ON p.id = td.predecessor_task_id
		WHERE td.successor_task_id = $1
		ORDER BY p.created_at ASC`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer depRows.Close()
	for depRows.Next() {
		var d contextDependency
		if err := depRows.Scan(&d.TaskID, &d.Title, &d.ResultMD, &d.Completed); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.Dependencies = append(ctxOut.Dependencies, d)
	}

	parentRows, err := s.db.QueryContext(r.Context(), `
		WITH RECURSIVE ancestors AS (
			SELECT t.id, t.project_id, t.parent_task_id, t.title, t.spec_md, t.result_md, t.status, t.display_order, t.created_at, t.started_at, t.done_at, 0 AS depth
			FROM tasks t
			WHERE t.id = (SELECT parent_task_id FROM tasks WHERE id = $1)
			UNION ALL
			SELECT p.id, p.project_id, p.parent_task_id, p.title, p.spec_md, p.result_md, p.status, p.display_order, p.created_at, p.started_at, p.done_at, a.depth + 1
			FROM tasks p
			JOIN ancestors a ON p.id = a.parent_task_id
		)
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, display_order, created_at, started_at, done_at
		FROM ancestors
		ORDER BY depth DESC`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer parentRows.Close()
	for parentRows.Next() {
		var p task
		if err := parentRows.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.SpecMD, &p.ResultMD, &p.Status, &p.DisplayOrder, &p.CreatedAt, &p.StartedAt, &p.DoneAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.ParentChain = append(ctxOut.ParentChain, p)
	}

	writeJSON(w, http.StatusOK, ctxOut)
}
