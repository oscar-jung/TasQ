package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
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

// streamProjectEvents handles GET /projects/:project_id/stream via Server-Sent Events.
func (s *server) streamProjectEvents(w http.ResponseWriter, r *http.Request, projectID int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	lastID := int64(0)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			lastID = parsed
		}
	}
	if raw := r.URL.Query().Get("last_event_id"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > lastID {
			lastID = parsed
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	fmt.Fprintf(w, ": connected project=%d\n\n", projectID)
	flusher.Flush()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events, err := s.fetchProjectEventsAfter(r.Context(), projectID, lastID, 100)
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", mustMarshalSSEData(map[string]any{"message": err.Error()}))
				flusher.Flush()
				continue
			}
			if len(events) == 0 {
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
				continue
			}
			for _, event := range events {
				lastID = event.ID
				fmt.Fprintf(w, "id: %d\n", event.ID)
				fmt.Fprint(w, "event: task-event\n")
				fmt.Fprintf(w, "data: %s\n\n", mustMarshalSSEData(event))
			}
			flusher.Flush()
		}
	}
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

func (s *server) fetchProjectEventsAfter(ctx context.Context, projectID, lastID int64, limit int) ([]taskEventSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, project_id, event_type, actor_type, actor_id, payload_json, created_at
		FROM task_events
		WHERE project_id = $1
		  AND id > $2
		ORDER BY id ASC
		LIMIT $3`, projectID, lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]taskEventSummary, 0, limit)
	for rows.Next() {
		var e taskEventSummary
		if err := rows.Scan(&e.ID, &e.TaskID, &e.ProjectID, &e.EventType, &e.ActorType, &e.ActorID, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = normalizeEventPayload(e.Payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func mustMarshalSSEData(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return `{"message":"marshal error"}`
	}
	return string(encoded)
}
