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

type exhaustedTaskAlert struct {
	TaskID       int64  `json:"task_id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	AttemptsUsed int    `json:"attempts_used"`
	MaxAttempts  int    `json:"max_attempts"`
}

type runtimeAlertsReport struct {
	ProjectID              int64                `json:"project_id"`
	HeartbeatStaleSeconds  int                  `json:"heartbeat_stale_seconds"`
	TotalTasks             int                  `json:"total_tasks"`
	DoneTasks              int                  `json:"done_tasks"`
	UnfinishedTasks        int                  `json:"unfinished_tasks"`
	ClaimableTasks         int                  `json:"claimable_tasks"`
	BlockedPlannedTasks    int                  `json:"blocked_planned_tasks"`
	StaleActiveClaims      []runtimeClaimAlert  `json:"stale_active_claims"`
	HeartbeatOverdueClaims []runtimeClaimAlert  `json:"heartbeat_overdue_claims"`
	OrphanInProgressTasks  []orphanTaskAlert    `json:"orphan_in_progress_tasks"`
	ExhaustedTasks         []exhaustedTaskAlert `json:"exhausted_tasks"`
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
		TotalTasks:             0,
		DoneTasks:              0,
		UnfinishedTasks:        0,
		ClaimableTasks:         0,
		BlockedPlannedTasks:    0,
		StaleActiveClaims:      []runtimeClaimAlert{},
		HeartbeatOverdueClaims: []runtimeClaimAlert{},
		OrphanInProgressTasks:  []orphanTaskAlert{},
		ExhaustedTasks:         []exhaustedTaskAlert{},
	}

	var plannedTasks int
	var exhaustedPlannedTasks int
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT
			COUNT(1) AS total_tasks,
			COUNT(1) FILTER (WHERE status = 'done') AS done_tasks,
			COUNT(1) FILTER (WHERE status <> 'done') AS unfinished_tasks,
			COUNT(1) FILTER (WHERE status = 'planned') AS planned_tasks,
			COUNT(1) FILTER (
				WHERE status = 'planned'
				  AND (
					SELECT COUNT(1)
					FROM task_claims c
					WHERE c.task_id = tasks.id
				  ) >= max_attempts
			) AS exhausted_planned_tasks
		FROM tasks
		WHERE project_id = $1`, projectID).
		Scan(&report.TotalTasks, &report.DoneTasks, &report.UnfinishedTasks, &plannedTasks, &exhaustedPlannedTasks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(1)
		FROM tasks t
		WHERE t.project_id = $1
		  AND t.status = 'planned'
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
			SELECT 1
			FROM task_claims c
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
		  )`, projectID).
		Scan(&report.ClaimableTasks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	report.BlockedPlannedTasks = plannedTasks - report.ClaimableTasks - exhaustedPlannedTasks
	if report.BlockedPlannedTasks < 0 {
		report.BlockedPlannedTasks = 0
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

	exhaustedRows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.title, t.status, COUNT(c.id) AS attempts_used, t.max_attempts
		FROM tasks t
		LEFT JOIN task_claims c ON c.task_id = t.id
		WHERE t.project_id = $1
		  AND t.status <> 'done'
		GROUP BY t.id, t.title, t.status, t.max_attempts
		HAVING COUNT(c.id) >= t.max_attempts
		ORDER BY t.created_at ASC, t.id ASC`, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for exhaustedRows.Next() {
		var alert exhaustedTaskAlert
		if err := exhaustedRows.Scan(&alert.TaskID, &alert.Title, &alert.Status, &alert.AttemptsUsed, &alert.MaxAttempts); err != nil {
			exhaustedRows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		report.ExhaustedTasks = append(report.ExhaustedTasks, alert)
	}
	exhaustedRows.Close()

	writeJSON(w, http.StatusOK, report)
}
