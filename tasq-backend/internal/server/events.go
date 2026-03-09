package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// listProjectEvents handles GET /projects/:project_id/events.
func (s *server) listProjectEvents(w http.ResponseWriter, r *http.Request, projectID int64) {
	limit := readEventLimit(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, task_id, project_id, event_type, actor_type, actor_id, payload_json, created_at
		FROM task_events
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, projectID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	events := make([]taskEventSummary, 0, limit)
	for rows.Next() {
		var e taskEventSummary
		if err := rows.Scan(&e.ID, &e.TaskID, &e.ProjectID, &e.EventType, &e.ActorType, &e.ActorID, &e.Payload, &e.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.Payload = normalizeEventPayload(e.Payload)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"project_id": projectID,
		"events":     events,
	})
}

// listTaskEvents handles GET /tasks/:task_id/events.
func (s *server) listTaskEvents(w http.ResponseWriter, r *http.Request, taskID int64) {
	var projectID int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	limit := readEventLimit(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, task_id, project_id, event_type, actor_type, actor_id, payload_json, created_at
		FROM task_events
		WHERE task_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, taskID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	events := make([]taskEventSummary, 0, limit)
	for rows.Next() {
		var e taskEventSummary
		if err := rows.Scan(&e.ID, &e.TaskID, &e.ProjectID, &e.EventType, &e.ActorType, &e.ActorID, &e.Payload, &e.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.Payload = normalizeEventPayload(e.Payload)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":    taskID,
		"project_id": projectID,
		"events":     events,
	})
}

func readEventLimit(r *http.Request) int {
	const (
		defaultLimit = 50
		maxLimit     = 200
	)
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func normalizeEventPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
