package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	pq "github.com/lib/pq"
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
	if !s.requireProjectAccess(w, r, req.ProjectID, "task:claim", false) {
		return
	}
	if !s.requireAgentIdentity(w, r, req.AgentID) {
		return
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 120
	}
	capabilities := normalizeCapabilities(req.Capabilities)

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := s.reconcileStaleClaimsTx(r.Context(), tx, req.ProjectID, "system", "lease-reaper"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var t task
	err = tx.QueryRowContext(r.Context(), `
		SELECT t.id, t.project_id, t.parent_task_id, t.title, t.spec_md, t.result_md, t.status, t.max_attempts, t.display_order, t.created_at, t.started_at, t.done_at
		FROM tasks t
		WHERE t.project_id = $1
		  AND t.status = 'planned'
		  AND (
			COALESCE(array_length(t.required_capabilities, 1), 0) = 0
			OR t.required_capabilities <@ $2::text[]
		  )
		  AND (
			t.parent_task_id IS NULL
			OR EXISTS (
				SELECT 1
				FROM tasks parent
				WHERE parent.id = t.parent_task_id
				  AND parent.status = 'done'
			)
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM task_claims c
			WHERE c.task_id = t.id
			  AND c.status = 'active'
			  AND c.lease_until > NOW()
		  )
		  AND (
			SELECT COUNT(1)
			FROM task_claims c
			WHERE c.task_id = t.id
		) < t.max_attempts
		  AND NOT EXISTS (
			SELECT 1
			FROM task_dependencies td
			JOIN tasks p ON p.id = td.predecessor_task_id
			WHERE td.successor_task_id = t.id
			  AND p.status <> 'done'
		  )
		ORDER BY t.display_order ASC, t.created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, req.ProjectID, pq.Array(capabilities)).
		Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.MaxAttempts, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil, "message": "no claimable task"})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	claimToken, err := generateClaimToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var claimID int64
	var attemptNo int
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO task_claims(task_id, claimed_by_type, claimed_by_id, lease_until, heartbeat_at, attempt_no, status)
		VALUES (
			$1,
			'agent',
			$2,
			NOW() + ($3::text || ' seconds')::interval,
			NOW(),
			COALESCE((SELECT MAX(attempt_no) + 1 FROM task_claims WHERE task_id = $1), 1),
			'active'
		)
		RETURNING id, attempt_no`,
		t.ID, req.AgentID, req.LeaseSeconds).Scan(&claimID, &attemptNo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		UPDATE task_claims
		SET claim_token = $2
		WHERE id = $1`, claimID, claimToken); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var runID int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO task_runs(task_id, agent_id, attempt_no, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id`, t.ID, req.AgentID, attemptNo).Scan(&runID)
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
	t.Status = "in_progress"

	if err := logEvent(r.Context(), tx, t.ID, "task.claimed", "agent", req.AgentID, map[string]any{
		"claim_id":      claimID,
		"claim_token":   claimToken,
		"attempt_no":    attemptNo,
		"task_run_id":   runID,
		"lease_seconds": req.LeaseSeconds,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task": t,
		"claim": map[string]any{
			"id":            claimID,
			"token":         claimToken,
			"attempt_no":    attemptNo,
			"task_run_id":   runID,
			"lease_seconds": req.LeaseSeconds,
		},
	})
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
	if !s.requireAgentIdentity(w, r, req.AgentID) {
		return
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 120
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claim, err := getActiveClaim(r.Context(), tx, taskID, req.AgentID, req.ClaimToken)
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
		SET lease_until = NOW() + ($2::text || ' seconds')::interval,
			heartbeat_at = NOW()
		WHERE id = $1`, claim.ID, req.LeaseSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.claim.heartbeat", "agent", req.AgentID, map[string]any{
		"claim_id":      claim.ID,
		"attempt_no":    claim.AttemptNo,
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

// saveTaskCheckpoint stores a mid-run note on the active task run.
func (s *server) saveTaskCheckpoint(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req checkpointReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Note) == "" {
		http.Error(w, "note is required", http.StatusBadRequest)
		return
	}
	if !s.requireAgentIdentity(w, r, req.AgentID) {
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claim, err := getActiveClaim(r.Context(), tx, taskID, req.AgentID, req.ClaimToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "active claim not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	checkpointPayload, err := json.Marshal(map[string]any{
		"checkpoint": map[string]any{
			"note":       strings.TrimSpace(req.Note),
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		WITH target AS (
			SELECT id
			FROM task_runs
			WHERE task_id = $1
			  AND agent_id = $2
			  AND attempt_no = $3
			  AND status = 'running'
			ORDER BY started_at DESC
			LIMIT 1
			FOR UPDATE
		)
		UPDATE task_runs
		SET result_payload_json = COALESCE(result_payload_json, '{}'::jsonb) || $4::jsonb
		WHERE id IN (SELECT id FROM target)`,
		taskID, req.AgentID, claim.AttemptNo, string(checkpointPayload))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "running task_run not found", http.StatusConflict)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.checkpoint.saved", "agent", req.AgentID, map[string]any{
		"claim_id":   claim.ID,
		"attempt_no": claim.AttemptNo,
		"note":       strings.TrimSpace(req.Note),
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
	if !s.requireAgentIdentity(w, r, req.AgentID) {
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

	claim, err := getActiveClaim(r.Context(), tx, taskID, req.AgentID, req.ClaimToken)
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
		WHERE id = $1`, claim.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := finishLatestTaskRun(r.Context(), tx, taskID, req.AgentID, claim.AttemptNo, "released", map[string]any{
		"to_status":   req.ToStatus,
		"reason":      "manual_release",
		"resume_hint": "Resume by claiming the task again and checking the latest task context before editing.",
	}); err != nil {
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
		"claim_id":   claim.ID,
		"attempt_no": claim.AttemptNo,
		"to_status":  req.ToStatus,
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
	if !s.requireAgentIdentity(w, r, req.AgentID) {
		return
	}
	resultPayload, err := parseRequiredResultPayload(req.ResultJSON, req.ResultPayloadVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateResultMarkdown(req.ResultMD); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claim, err := getActiveClaim(r.Context(), tx, taskID, req.AgentID, req.ClaimToken)
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
		WHERE id = $1`, claim.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := finishLatestTaskRun(r.Context(), tx, taskID, req.AgentID, claim.AttemptNo, "completed", resultPayload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.completed", "agent", req.AgentID, map[string]any{
		"claim_id":   claim.ID,
		"attempt_no": claim.AttemptNo,
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

// failTask handles POST /tasks/:task_id/fail.
func (s *server) failTask(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req failReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}
	if !s.requireAgentIdentity(w, r, req.AgentID) {
		return
	}
	resultPayload, err := parseRequiredResultPayload(req.ResultJSON, req.ResultPayloadVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateResultMarkdown(req.ResultMD); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resultPayload["reason"] = req.Reason

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	claim, err := getActiveClaim(r.Context(), tx, taskID, req.AgentID, req.ClaimToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "active claim not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE tasks
		SET status = 'failed',
			result_md = CASE WHEN $2 <> '' THEN $2 ELSE result_md END,
			updated_at = NOW()
		WHERE id = $1`, taskID, req.ResultMD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		UPDATE task_claims
		SET status = 'released', released_at = NOW()
		WHERE id = $1`, claim.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := finishLatestTaskRun(r.Context(), tx, taskID, req.AgentID, claim.AttemptNo, "failed", resultPayload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := logEvent(r.Context(), tx, taskID, "task.failed", "agent", req.AgentID, map[string]any{
		"claim_id":   claim.ID,
		"attempt_no": claim.AttemptNo,
		"reason":     req.Reason,
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

type activeClaim struct {
	ID        int64
	AttemptNo int
}

// getActiveClaim fetches the current active lease for an agent-task pair.
func getActiveClaim(ctx context.Context, tx *sql.Tx, taskID int64, agentID, claimToken string) (activeClaim, error) {
	var claim activeClaim
	err := tx.QueryRowContext(ctx, `
		SELECT id, attempt_no
		FROM task_claims
		WHERE task_id = $1
		  AND claimed_by_id = $2
		  AND ($3 = '' OR claim_token = $3)
		  AND status = 'active'
		  AND lease_until > NOW()
		ORDER BY claimed_at DESC
		LIMIT 1
		FOR UPDATE`, taskID, agentID, claimToken).Scan(&claim.ID, &claim.AttemptNo)
	if err != nil {
		return activeClaim{}, err
	}
	return claim, nil
}

func finishLatestTaskRun(ctx context.Context, tx *sql.Tx, taskID int64, agentID string, attemptNo int, status string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal run payload: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		WITH target AS (
			SELECT id
			FROM task_runs
			WHERE task_id = $1
			  AND agent_id = $2
			  AND attempt_no = $3
			  AND status = 'running'
			ORDER BY started_at DESC
			LIMIT 1
			FOR UPDATE
		)
		UPDATE task_runs
		SET status = $4,
			result_payload_json = $5::jsonb,
			finished_at = NOW()
		WHERE id IN (SELECT id FROM target)`,
		taskID, agentID, attemptNo, status, string(payloadJSON))
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("running task_run not found (task=%d agent=%s attempt=%d)", taskID, agentID, attemptNo)
	}
	return nil
}

func generateClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate claim token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func parseRequiredResultPayload(raw json.RawMessage, version string) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("result_payload is required")
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid result_payload json")
	}

	schemaVersion := strings.ToLower(strings.TrimSpace(version))
	if schemaVersion == "" {
		schemaVersion = "v2"
	}
	switch schemaVersion {
	case "v1":
		required := []string{
			"summary",
			"changes",
			"paths",
			"commands",
			"tests",
			"artifacts",
			"next_risks",
		}
		for _, key := range required {
			v, ok := payload[key]
			if !ok {
				return nil, fmt.Errorf("result_payload.%s is required", key)
			}
			s, isString := v.(string)
			if !isString {
				return nil, fmt.Errorf("result_payload.%s must be a string in v1", key)
			}
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("result_payload.%s must not be empty", key)
			}
		}
	case "v2":
		if err := requireNonEmptyString(payload, "summary"); err != nil {
			return nil, err
		}
		arrayKeys := []string{"changes", "paths", "commands", "tests", "artifacts", "next_risks"}
		for _, key := range arrayKeys {
			if err := requireStringArray(payload, key); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported result_payload_version: %s", schemaVersion)
	}
	payload["schema_version"] = schemaVersion
	return payload, nil
}

func requireNonEmptyString(payload map[string]any, key string) error {
	v, ok := payload[key]
	if !ok {
		return fmt.Errorf("result_payload.%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("result_payload.%s must be a string", key)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("result_payload.%s must not be empty", key)
	}
	return nil
}

func requireStringArray(payload map[string]any, key string) error {
	v, ok := payload[key]
	if !ok {
		return fmt.Errorf("result_payload.%s is required", key)
	}
	items, ok := v.([]any)
	if !ok {
		return fmt.Errorf("result_payload.%s must be an array of strings", key)
	}
	if len(items) == 0 {
		return fmt.Errorf("result_payload.%s must contain at least one entry", key)
	}
	for idx, item := range items {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("result_payload.%s[%d] must be a non-empty string", key, idx)
		}
	}
	return nil
}

func validateResultMarkdown(resultMD string) error {
	trimmed := strings.TrimSpace(resultMD)
	if trimmed == "" {
		return fmt.Errorf("result_md is required and must use the handoff template")
	}
	if len([]rune(trimmed)) < 80 {
		return fmt.Errorf("result_md must be detailed enough for handoff (minimum 80 characters)")
	}
	requiredSections := []string{
		"## Summary",
		"## Changes",
		"## Verification",
		"## Risks",
	}
	missing := make([]string, 0)
	for _, section := range requiredSections {
		if !strings.Contains(trimmed, section) {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("result_md must include sections: %s", strings.Join(missing, ", "))
	}
	return nil
}

// getTaskContext handles GET /tasks/:task_id/context.
func (s *server) getTaskContext(w http.ResponseWriter, r *http.Request, taskID int64) {
	var ctxOut taskContext

	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, max_attempts, display_order, created_at, started_at, done_at
		FROM tasks
		WHERE id = $1`, taskID).
		Scan(&ctxOut.Task.ID, &ctxOut.Task.ProjectID, &ctxOut.Task.ParentID, &ctxOut.Task.Title, &ctxOut.Task.SpecMD, &ctxOut.Task.ResultMD, &ctxOut.Task.Status, &ctxOut.Task.MaxAttempts, &ctxOut.Task.DisplayOrder, &ctxOut.Task.CreatedAt, &ctxOut.Task.StartedAt, &ctxOut.Task.DoneAt)
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
			SELECT t.id, t.project_id, t.parent_task_id, t.title, t.spec_md, t.result_md, t.status, t.max_attempts, t.display_order, t.created_at, t.started_at, t.done_at, 0 AS depth
			FROM tasks t
			WHERE t.id = (SELECT parent_task_id FROM tasks WHERE id = $1)
			UNION ALL
			SELECT p.id, p.project_id, p.parent_task_id, p.title, p.spec_md, p.result_md, p.status, p.max_attempts, p.display_order, p.created_at, p.started_at, p.done_at, a.depth + 1
			FROM tasks p
			JOIN ancestors a ON p.id = a.parent_task_id
		)
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, max_attempts, display_order, created_at, started_at, done_at
		FROM ancestors
		ORDER BY depth DESC`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer parentRows.Close()
	for parentRows.Next() {
		var p task
		if err := parentRows.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.SpecMD, &p.ResultMD, &p.Status, &p.MaxAttempts, &p.DisplayOrder, &p.CreatedAt, &p.StartedAt, &p.DoneAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.ParentChain = append(ctxOut.ParentChain, p)
	}

	runRows, err := s.db.QueryContext(r.Context(), `
		SELECT id, agent_id, attempt_no, status, started_at, finished_at, result_payload_json
		FROM task_runs
		WHERE task_id = $1
		ORDER BY started_at DESC
		LIMIT 10`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer runRows.Close()
	for runRows.Next() {
		var run taskRunSummary
		if err := runRows.Scan(&run.ID, &run.AgentID, &run.AttemptNo, &run.Status, &run.StartedAt, &run.FinishedAt, &run.ResultJSON); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.RecentRuns = append(ctxOut.RecentRuns, run)
	}

	interruptedRows, err := s.db.QueryContext(r.Context(), `
		SELECT id,
			agent_id,
			attempt_no,
			finished_at,
			COALESCE(result_payload_json->>'reason', 'released'),
			COALESCE(result_payload_json->>'resume_hint', ''),
			COALESCE(result_payload_json->>'to_status', ''),
			COALESCE(result_payload_json->'checkpoint'->>'note', '')
		FROM task_runs
		WHERE task_id = $1
		  AND status = 'released'
		ORDER BY finished_at DESC NULLS LAST, started_at DESC
		LIMIT 5`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer interruptedRows.Close()
	for interruptedRows.Next() {
		var run interruptedRunSummary
		if err := interruptedRows.Scan(&run.ID, &run.AgentID, &run.AttemptNo, &run.FinishedAt, &run.Reason, &run.ResumeHint, &run.ToStatus, &run.Checkpoint); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.InterruptedRuns = append(ctxOut.InterruptedRuns, run)
	}

	gitRows, err := s.db.QueryContext(r.Context(), `
		SELECT id, repo, branch, base_commit, commit_sha, created_at
		FROM task_git_refs
		WHERE task_id = $1
		ORDER BY created_at DESC
		LIMIT 10`, taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer gitRows.Close()
	for gitRows.Next() {
		var ref taskGitRefSummary
		if err := gitRows.Scan(&ref.ID, &ref.Repo, &ref.Branch, &ref.BaseCommit, &ref.CommitSHA, &ref.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ctxOut.GitRefs = append(ctxOut.GitRefs, ref)
	}

	writeJSON(w, http.StatusOK, ctxOut)
}
