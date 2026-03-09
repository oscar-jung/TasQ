package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// createProject handles POST /projects.
func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	var p project
	err := s.db.QueryRowContext(r.Context(), `
		INSERT INTO projects(name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at`,
		req.Name, req.Description,
	).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

// updateProject handles PATCH /projects/:project_id.
func (s *server) updateProject(w http.ResponseWriter, r *http.Request, projectID int64) {
	var req updateProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	res, err := s.db.ExecContext(r.Context(), `
		UPDATE projects
		SET name = $2,
			updated_at = NOW()
		WHERE id = $1`, projectID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listProjects handles GET /projects.
func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, description, created_at
		FROM projects
		ORDER BY created_at ASC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]project, 0)
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// listProjectTasks handles GET /projects/:project_id/tasks.
func (s *server) listProjectTasks(w http.ResponseWriter, r *http.Request, projectID int64) {
	tasks, err := s.fetchProjectTasks(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// getProjectTaskTree handles GET /projects/:project_id/tasks/tree.
func (s *server) getProjectTaskTree(w http.ResponseWriter, r *http.Request, projectID int64) {
	tasks, err := s.fetchProjectTasks(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, buildTaskTree(tasks))
}

// fetchProjectTasks loads all tasks in a project with stable ordering.
func (s *server) fetchProjectTasks(ctx context.Context, projectID int64) ([]task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, max_attempts, display_order, created_at, started_at, done_at
		FROM tasks
		WHERE project_id = $1
		ORDER BY display_order ASC, created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]task, 0)
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.MaxAttempts, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// buildTaskTree converts a flat task list into nested tree nodes.
func buildTaskTree(tasks []task) []taskTreeNode {
	byParent := map[int64][]task{}
	roots := make([]task, 0)
	for _, t := range tasks {
		if t.ParentID == nil {
			roots = append(roots, t)
			continue
		}
		byParent[*t.ParentID] = append(byParent[*t.ParentID], t)
	}

	for pid := range byParent {
		sort.Slice(byParent[pid], func(i, j int) bool {
			if byParent[pid][i].DisplayOrder == byParent[pid][j].DisplayOrder {
				return byParent[pid][i].CreatedAt.Before(byParent[pid][j].CreatedAt)
			}
			return byParent[pid][i].DisplayOrder < byParent[pid][j].DisplayOrder
		})
	}

	sort.Slice(roots, func(i, j int) bool {
		if roots[i].DisplayOrder == roots[j].DisplayOrder {
			return roots[i].CreatedAt.Before(roots[j].CreatedAt)
		}
		return roots[i].DisplayOrder < roots[j].DisplayOrder
	})

	var makeNode func(t task) taskTreeNode
	makeNode = func(t task) taskTreeNode {
		n := taskTreeNode{task: t, Children: make([]taskTreeNode, 0)}
		for _, child := range byParent[t.ID] {
			n.Children = append(n.Children, makeNode(child))
		}
		return n
	}

	out := make([]taskTreeNode, 0, len(roots))
	for _, root := range roots {
		out = append(out, makeNode(root))
	}
	return out
}
