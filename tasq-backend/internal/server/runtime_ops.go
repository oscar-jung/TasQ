package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"
)

type runtimeClaimAlert struct {
	ClaimID               int64      `json:"claim_id"`
	TaskID                int64      `json:"task_id"`
	AgentID               string     `json:"agent_id"`
	LeaseUntil            time.Time  `json:"lease_until"`
	HeartbeatAt           *time.Time `json:"heartbeat_at"`
	SecondsSinceHeartbeat int64      `json:"seconds_since_heartbeat"`
}

type orphanTaskAlert struct {
	TaskID    int64      `json:"task_id"`
	StartedAt *time.Time `json:"started_at"`
}

type runtimeAlertsReport struct {
	ProjectID              int64               `json:"project_id"`
	HeartbeatStaleSeconds  int                 `json:"heartbeat_stale_seconds"`
	StaleActiveClaims      []runtimeClaimAlert `json:"stale_active_claims"`
	HeartbeatOverdueClaims []runtimeClaimAlert `json:"heartbeat_overdue_claims"`
	OrphanInProgressTasks  []orphanTaskAlert   `json:"orphan_in_progress_tasks"`
}

func (s *server) runClaimReconciler(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := s.reconcileStaleClaimsAllProjects(context.Background()); err != nil {
			log.Printf("claim reconciler error: %v", err)
		}
	}
}

func (s *server) reconcileStaleClaimsAllProjects(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT t.project_id
		FROM task_claims c
		JOIN tasks t ON t.id = c.task_id
		WHERE c.status = 'active'
		  AND c.lease_until <= NOW()`)
	if err != nil {
		return err
	}
	defer rows.Close()

	projectIDs := make([]int64, 0, 16)
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return err
		}
		projectIDs = append(projectIDs, pid)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, pid := range projectIDs {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		report, recErr := s.reconcileStaleClaimsTx(ctx, tx, pid, "system", "lease-reaper")
		if recErr != nil {
			_ = tx.Rollback()
			return recErr
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(report.ReleasedClaimIDs) > 0 || len(report.ResetTaskIDs) > 0 {
			log.Printf(
				"claim reconciler project=%d released_claims=%d reset_tasks=%d released_runs=%d",
				pid, len(report.ReleasedClaimIDs), len(report.ResetTaskIDs), len(report.ReleasedTaskRunIDs),
			)
		}
	}
	return nil
}

// getProjectRuntimeAlerts handles GET /projects/:project_id/runtime-alerts.
func (s *server) getProjectRuntimeAlerts(w http.ResponseWriter, r *http.Request, projectID int64) {
	staleSec := s.defaultHeartbeatStaleSec
	if raw := r.URL.Query().Get("heartbeat_stale_seconds"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			staleSec = parsed
		}
	}

	report := runtimeAlertsReport{
		ProjectID:              projectID,
		HeartbeatStaleSeconds:  staleSec,
		StaleActiveClaims:      []runtimeClaimAlert{},
		HeartbeatOverdueClaims: []runtimeClaimAlert{},
		OrphanInProgressTasks:  []orphanTaskAlert{},
	}

	staleRows, err := s.db.QueryContext(r.Context(), `
		SELECT c.id, c.task_id, c.claimed_by_id, c.lease_until, c.heartbeat_at
		FROM task_claims c
		JOIN tasks t ON t.id = c.task_id
		WHERE t.project_id = $1
		  AND c.status = 'active'
		  AND c.lease_until <= NOW()
		ORDER BY c.lease_until ASC`, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for staleRows.Next() {
		var alert runtimeClaimAlert
		var heartbeat sql.NullTime
		if err := staleRows.Scan(&alert.ClaimID, &alert.TaskID, &alert.AgentID, &alert.LeaseUntil, &heartbeat); err != nil {
			staleRows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if heartbeat.Valid {
			t := heartbeat.Time
			alert.HeartbeatAt = &t
			alert.SecondsSinceHeartbeat = int64(time.Since(t).Seconds())
		}
		report.StaleActiveClaims = append(report.StaleActiveClaims, alert)
	}
	staleRows.Close()

	overdueRows, err := s.db.QueryContext(r.Context(), `
		SELECT c.id, c.task_id, c.claimed_by_id, c.lease_until, c.heartbeat_at
		FROM task_claims c
		JOIN tasks t ON t.id = c.task_id
		WHERE t.project_id = $1
		  AND c.status = 'active'
		  AND c.heartbeat_at <= NOW() - ($2::text || ' seconds')::interval
		ORDER BY c.heartbeat_at ASC`, projectID, staleSec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for overdueRows.Next() {
		var alert runtimeClaimAlert
		var heartbeat sql.NullTime
		if err := overdueRows.Scan(&alert.ClaimID, &alert.TaskID, &alert.AgentID, &alert.LeaseUntil, &heartbeat); err != nil {
			overdueRows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if heartbeat.Valid {
			t := heartbeat.Time
			alert.HeartbeatAt = &t
			alert.SecondsSinceHeartbeat = int64(time.Since(t).Seconds())
		}
		report.HeartbeatOverdueClaims = append(report.HeartbeatOverdueClaims, alert)
	}
	overdueRows.Close()

	orphanRows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.started_at
		FROM tasks t
		WHERE t.project_id = $1
		  AND t.status = 'in_progress'
		  AND NOT EXISTS (
			SELECT 1
			FROM task_claims c
			WHERE c.task_id = t.id
			  AND c.status = 'active'
			  AND c.lease_until > NOW()
		  )
		ORDER BY t.started_at ASC NULLS LAST, t.id ASC`, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for orphanRows.Next() {
		var alert orphanTaskAlert
		if err := orphanRows.Scan(&alert.TaskID, &alert.StartedAt); err != nil {
			orphanRows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		report.OrphanInProgressTasks = append(report.OrphanInProgressTasks, alert)
	}
	orphanRows.Close()

	writeJSON(w, http.StatusOK, report)
}
