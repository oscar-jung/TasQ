package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
)

type staleClaimRow struct {
	ClaimID   int64
	TaskID    int64
	AgentID   string
	AttemptNo int
}

// reconcileProjectClaims handles POST /projects/:project_id/claims/reconcile.
func (s *server) reconcileProjectClaims(w http.ResponseWriter, r *http.Request, projectID int64) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	actorType, actorID := actorForEvent(r)
	report, err := s.reconcileStaleClaimsTx(r.Context(), tx, projectID, actorType, actorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// reconcileStaleClaimsTx releases expired active claims and repairs stale task/task_run states.
func (s *server) reconcileStaleClaimsTx(ctx context.Context, tx *sql.Tx, projectID int64, actorType, actorID string) (staleClaimReconcileReport, error) {
	report := staleClaimReconcileReport{
		ProjectID:          projectID,
		ReleasedClaimIDs:   []int64{},
		ResetTaskIDs:       []int64{},
		ReleasedTaskRunIDs: []int64{},
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, c.task_id, c.claimed_by_id, c.attempt_no
		FROM task_claims c
		JOIN tasks t ON t.id = c.task_id
		WHERE t.project_id = $1
		  AND c.status = 'active'
		  AND c.lease_until <= NOW()
		FOR UPDATE`, projectID)
	if err != nil {
		return report, err
	}
	defer rows.Close()

	staleRows := make([]staleClaimRow, 0, 16)
	for rows.Next() {
		var row staleClaimRow
		if err := rows.Scan(&row.ClaimID, &row.TaskID, &row.AgentID, &row.AttemptNo); err != nil {
			return report, err
		}
		staleRows = append(staleRows, row)
	}
	if err := rows.Err(); err != nil {
		return report, err
	}

	for _, row := range staleRows {
		res, err := tx.ExecContext(ctx, `
			UPDATE task_claims
			SET status = 'released',
				released_at = COALESCE(released_at, NOW())
			WHERE id = $1
			  AND status = 'active'`, row.ClaimID)
		if err != nil {
			return report, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			continue
		}

		report.ReleasedClaimIDs = append(report.ReleasedClaimIDs, row.ClaimID)

		runPayload, _ := json.Marshal(map[string]any{
			"reason": "lease_expired_reconcile",
		})
		runRes, err := tx.ExecContext(ctx, `
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
			SET status = 'released',
				result_payload_json = $4::jsonb,
				finished_at = NOW()
			WHERE id IN (SELECT id FROM target)`,
			row.TaskID, row.AgentID, row.AttemptNo, string(runPayload))
		if err != nil {
			return report, err
		}
		runAffected, _ := runRes.RowsAffected()
		if runAffected > 0 {
			var runID int64
			if err := tx.QueryRowContext(ctx, `
				SELECT id
				FROM task_runs
				WHERE task_id = $1
				  AND agent_id = $2
				  AND attempt_no = $3
				ORDER BY started_at DESC
				LIMIT 1`, row.TaskID, row.AgentID, row.AttemptNo).Scan(&runID); err == nil {
				report.ReleasedTaskRunIDs = append(report.ReleasedTaskRunIDs, runID)
			}
		}

		var hasActive int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(1)
			FROM task_claims
			WHERE task_id = $1
			  AND status = 'active'
			  AND lease_until > NOW()`, row.TaskID).Scan(&hasActive); err != nil {
			return report, err
		}
		if hasActive > 0 {
			continue
		}

		taskRes, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'planned',
				updated_at = NOW()
			WHERE id = $1
			  AND project_id = $2
			  AND status = 'in_progress'`, row.TaskID, projectID)
		if err != nil {
			return report, err
		}
		taskAffected, _ := taskRes.RowsAffected()
		if taskAffected > 0 {
			report.ResetTaskIDs = append(report.ResetTaskIDs, row.TaskID)
			if err := logEvent(ctx, tx, row.TaskID, "task.reconciled.stale_claim", actorType, actorID, map[string]any{
				"released_claim_id": row.ClaimID,
				"reason":            "lease_expired_reconcile",
			}); err != nil {
				return report, err
			}
		}
	}

	return report, nil
}
