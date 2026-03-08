package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"

	pq "github.com/lib/pq"
)

type validateTreeReq struct {
	Cleanse bool `json:"cleanse"`
}

type treeValidationReport struct {
	ProjectID              int64   `json:"project_id"`
	CheckedTasks           int     `json:"checked_tasks"`
	MissingParentTaskIDs   []int64 `json:"missing_parent_task_ids"`
	CycleTaskIDs           []int64 `json:"cycle_task_ids"`
	StatusViolationTaskIDs []int64 `json:"status_violation_task_ids"`
	CleansedTaskIDs        []int64 `json:"cleansed_task_ids"`
	Valid                  bool    `json:"valid"`
	Cleansed               bool    `json:"cleansed"`
}

// validateProjectTree handles POST /projects/:project_id/tasks/validate.
func (s *server) validateProjectTree(w http.ResponseWriter, r *http.Request, projectID int64) {
	var req validateTreeReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	report, err := s.validateAndOptionallyCleanseProjectTree(r.Context(), projectID, req.Cleanse)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// maybeRunTreeGuard runs optional guard mode after mutating operations.
func (s *server) maybeRunTreeGuard(ctx context.Context, projectID int64) {
	if s.treeGuardMode == "off" {
		return
	}
	cleanse := s.treeGuardMode == "cleanse"
	report, err := s.validateAndOptionallyCleanseProjectTree(ctx, projectID, cleanse)
	if err != nil {
		log.Printf("tree guard failed (project=%d mode=%s): %v", projectID, s.treeGuardMode, err)
		return
	}
	if !report.Valid || len(report.CleansedTaskIDs) > 0 {
		log.Printf(
			"tree guard report (project=%d mode=%s valid=%t missing=%d cycles=%d status_violations=%d cleansed=%d)",
			projectID,
			s.treeGuardMode,
			report.Valid,
			len(report.MissingParentTaskIDs),
			len(report.CycleTaskIDs),
			len(report.StatusViolationTaskIDs),
			len(report.CleansedTaskIDs),
		)
	}
}

// validateAndOptionallyCleanseProjectTree validates topology and status constraints.
func (s *server) validateAndOptionallyCleanseProjectTree(ctx context.Context, projectID int64, cleanse bool) (treeValidationReport, error) {
	report := treeValidationReport{
		ProjectID:              projectID,
		MissingParentTaskIDs:   make([]int64, 0),
		CycleTaskIDs:           make([]int64, 0),
		StatusViolationTaskIDs: make([]int64, 0),
		CleansedTaskIDs:        make([]int64, 0),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()

	if err := lockProjectTopology(ctx, tx, projectID); err != nil {
		return report, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, parent_task_id, status
		FROM tasks
		WHERE project_id = $1
		FOR UPDATE`, projectID)
	if err != nil {
		return report, err
	}
	defer rows.Close()

	type lightweightTask struct {
		id       int64
		parentID *int64
		status   string
	}

	tasks := make([]lightweightTask, 0)
	byID := map[int64]lightweightTask{}
	for rows.Next() {
		var t lightweightTask
		if err := rows.Scan(&t.id, &t.parentID, &t.status); err != nil {
			return report, err
		}
		tasks = append(tasks, t)
		byID[t.id] = t
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	report.CheckedTasks = len(tasks)

	missingSet := map[int64]struct{}{}
	for _, t := range tasks {
		if t.parentID != nil {
			if _, ok := byID[*t.parentID]; !ok {
				missingSet[t.id] = struct{}{}
			}
		}
	}
	for id := range missingSet {
		report.MissingParentTaskIDs = append(report.MissingParentTaskIDs, id)
	}
	sort.Slice(report.MissingParentTaskIDs, func(i, j int) bool { return report.MissingParentTaskIDs[i] < report.MissingParentTaskIDs[j] })

	const (
		white = 0
		gray  = 1
		black = 2
	)
	visit := map[int64]int{}
	cycleSet := map[int64]struct{}{}
	var dfs func(id int64, stack map[int64]struct{})
	dfs = func(id int64, stack map[int64]struct{}) {
		visit[id] = gray
		stack[id] = struct{}{}
		current := byID[id]
		if current.parentID != nil {
			parentID := *current.parentID
			if _, ok := byID[parentID]; ok {
				state := visit[parentID]
				if state == gray {
					for taskID := range stack {
						cycleSet[taskID] = struct{}{}
					}
					cycleSet[parentID] = struct{}{}
				} else if state == white {
					dfs(parentID, stack)
				}
			}
		}
		delete(stack, id)
		visit[id] = black
	}
	for _, t := range tasks {
		if visit[t.id] == white {
			dfs(t.id, map[int64]struct{}{})
		}
	}
	for id := range cycleSet {
		report.CycleTaskIDs = append(report.CycleTaskIDs, id)
	}
	sort.Slice(report.CycleTaskIDs, func(i, j int) bool { return report.CycleTaskIDs[i] < report.CycleTaskIDs[j] })

	violatingSet := map[int64]struct{}{}
	for _, t := range tasks {
		if t.parentID == nil {
			continue
		}
		parent, ok := byID[*t.parentID]
		if !ok {
			continue
		}
		if parent.status != "done" && t.status != "planned" {
			violatingSet[t.id] = struct{}{}
		}
	}
	for id := range violatingSet {
		report.StatusViolationTaskIDs = append(report.StatusViolationTaskIDs, id)
	}
	sort.Slice(report.StatusViolationTaskIDs, func(i, j int) bool { return report.StatusViolationTaskIDs[i] < report.StatusViolationTaskIDs[j] })

	if cleanse && len(report.StatusViolationTaskIDs) > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'planned',
			    started_at = NULL,
			    done_at = NULL,
			    updated_at = NOW()
			WHERE project_id = $1
			  AND id = ANY($2::bigint[])`, projectID, pq.Array(report.StatusViolationTaskIDs)); err != nil {
			return report, err
		}
		report.Cleansed = true
		report.CleansedTaskIDs = append(report.CleansedTaskIDs, report.StatusViolationTaskIDs...)
	}

	report.Valid = len(report.MissingParentTaskIDs) == 0 && len(report.CycleTaskIDs) == 0 && len(report.StatusViolationTaskIDs) == 0

	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}

// fetchProjectIDByTaskID resolves a task to its project for post-action guards.
func (s *server) fetchProjectIDByTaskID(ctx context.Context, taskID int64) (int64, error) {
	var projectID int64
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&projectID); err != nil {
		return 0, err
	}
	return projectID, nil
}
