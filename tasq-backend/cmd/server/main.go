package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	pq "github.com/lib/pq"
)

type server struct {
	db *sql.DB
}

type project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type task struct {
	ID           int64      `json:"id"`
	ProjectID    int64      `json:"project_id"`
	ParentID     *int64     `json:"parent_task_id"`
	Title        string     `json:"title"`
	SpecMD       string     `json:"spec_md"`
	ResultMD     string     `json:"result_md"`
	Status       string     `json:"status"`
	DisplayOrder int64      `json:"display_order"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	DoneAt       *time.Time `json:"done_at"`
}

type taskTreeNode struct {
	task
	Children []taskTreeNode `json:"children"`
}

type taskDependency struct {
	TaskID    int64  `json:"task_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Direction string `json:"direction"`
}

type createProjectReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectReq struct {
	Name string `json:"name"`
}

type createTaskReq struct {
	ParentTaskID *int64 `json:"parent_task_id"`
	Title        string `json:"title"`
	SpecMD       string `json:"spec_md"`
}

type addDependencyReq struct {
	PredecessorTaskID int64 `json:"predecessor_task_id"`
}

type updateTaskStatusReq struct {
	Status   string `json:"status"`
	ResultMD string `json:"result_md"`
}

type updateTaskContentReq struct {
	Title    *string `json:"title"`
	SpecMD   *string `json:"spec_md"`
	ResultMD *string `json:"result_md"`
}

type claimNextReq struct {
	ProjectID    int64  `json:"project_id"`
	AgentID      string `json:"agent_id"`
	LeaseSeconds int64  `json:"lease_seconds"`
}

type heartbeatReq struct {
	AgentID      string `json:"agent_id"`
	LeaseSeconds int64  `json:"lease_seconds"`
}

type releaseReq struct {
	AgentID  string `json:"agent_id"`
	ToStatus string `json:"to_status"`
}

type completeReq struct {
	AgentID  string `json:"agent_id"`
	ResultMD string `json:"result_md"`
}

type moveTaskReq struct {
	NewParentTaskID *int64 `json:"new_parent_task_id"`
	NewIndex        *int   `json:"new_index"`
}

type reorderTasksReq struct {
	ProjectID    int64   `json:"project_id"`
	ParentTaskID *int64  `json:"parent_task_id"`
	OrderedTask  []int64 `json:"ordered_task_ids"`
}

type deleteTaskReq struct {
	Strategy string `json:"strategy"`
}

type contextDependency struct {
	TaskID    int64  `json:"task_id"`
	Title     string `json:"title"`
	ResultMD  string `json:"result_md"`
	Completed string `json:"completed_at"`
}

type taskContext struct {
	Task         task                `json:"task"`
	Dependencies []contextDependency `json:"dependencies"`
	ParentChain  []task              `json:"parent_chain"`
}

func main() {
	dbURL := getenv("DATABASE_URL", "postgres://tasq:tasq@db:5432/tasq?sslmode=disable")
	port := getenv("PORT", "8080")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	s := &server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/projects", s.projects)
	mux.HandleFunc("/projects/", s.projectsSubrouter)
	mux.HandleFunc("/tasks/reorder", s.reorderTasks)
	mux.HandleFunc("/tasks/", s.tasksSubrouter)
	mux.HandleFunc("/agents/claim-next", s.claimNext)

	h := withJSON(withCORS(mux))
	log.Printf("tasq-backend listening on :%s", port)
	if err := http.ListenAndServe(":"+port, h); err != nil {
		log.Fatal(err)
	}
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listProjects(w, r)
	case http.MethodPost:
		s.createProject(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) projectsSubrouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/projects/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid project id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 && r.Method == http.MethodPatch {
		s.updateProject(w, r, projectID)
		return
	}
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	if parts[1] == "tasks" {
		if len(parts) == 2 && r.Method == http.MethodPost {
			s.createTask(w, r, projectID)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			s.listProjectTasks(w, r, projectID)
			return
		}
		if len(parts) == 3 && parts[2] == "tree" && r.Method == http.MethodGet {
			s.getProjectTaskTree(w, r, projectID)
			return
		}
	}

	http.NotFound(w, r)
}

func (s *server) tasksSubrouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	taskID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}

	switch parts[1] {
	case "dependencies":
		if len(parts) == 2 {
			if r.Method == http.MethodPost {
				s.addDependency(w, r, taskID)
				return
			}
			if r.Method == http.MethodGet {
				s.listDependencies(w, r, taskID)
				return
			}
		}
		if len(parts) == 3 && r.Method == http.MethodDelete {
			predecessorID, parseErr := strconv.ParseInt(parts[2], 10, 64)
			if parseErr != nil {
				http.Error(w, "invalid predecessor id", http.StatusBadRequest)
				return
			}
			s.removeDependency(w, r, taskID, predecessorID)
			return
		}
	case "status":
		if r.Method == http.MethodPatch {
			s.updateTaskStatus(w, r, taskID)
			return
		}
	case "context":
		if r.Method == http.MethodGet {
			s.getTaskContext(w, r, taskID)
			return
		}
	case "heartbeat":
		if r.Method == http.MethodPost {
			s.heartbeatTaskClaim(w, r, taskID)
			return
		}
	case "release":
		if r.Method == http.MethodPost {
			s.releaseTaskClaim(w, r, taskID)
			return
		}
	case "complete":
		if r.Method == http.MethodPost {
			s.completeTask(w, r, taskID)
			return
		}
	case "move":
		if r.Method == http.MethodPost {
			s.moveTask(w, r, taskID)
			return
		}
	case "content":
		if r.Method == http.MethodPatch {
			s.updateTaskContent(w, r, taskID)
			return
		}
	case "delete":
		if r.Method == http.MethodPost {
			s.deleteTask(w, r, taskID)
			return
		}
	}

	http.NotFound(w, r)
}

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

	projects := make([]project, 0)
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		projects = append(projects, p)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *server) listProjectTasks(w http.ResponseWriter, r *http.Request, projectID int64) {
	tasks, err := s.fetchProjectTasks(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *server) getProjectTaskTree(w http.ResponseWriter, r *http.Request, projectID int64) {
	tasks, err := s.fetchProjectTasks(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, buildTaskTree(tasks))
}

func (s *server) fetchProjectTasks(ctx context.Context, projectID int64) ([]task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, parent_task_id, title, spec_md, result_md, status, display_order, created_at, started_at, done_at
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
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

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
		INSERT INTO tasks(project_id, parent_task_id, title, spec_md, status, display_order)
		VALUES ($1, $2, $3, $4, 'planned', COALESCE((
			SELECT MAX(display_order) + 1024
			FROM tasks
			WHERE project_id = $1
			  AND parent_task_id IS NOT DISTINCT FROM $2
		), 1024))
		RETURNING id, project_id, parent_task_id, title, spec_md, result_md, status, display_order, created_at, started_at, done_at`,
		projectID, req.ParentTaskID, req.Title, req.SpecMD,
	).Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Title, &t.SpecMD, &t.ResultMD, &t.Status, &t.DisplayOrder, &t.CreatedAt, &t.StartedAt, &t.DoneAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

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

func (s *server) updateTaskStatus(w http.ResponseWriter, r *http.Request, taskID int64) {
	var req updateTaskStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	status := strings.TrimSpace(req.Status)
	if status != "planned" && status != "in_progress" && status != "done" {
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

	if err := logEvent(r.Context(), tx, taskID, "task.status.updated", "human", "local-user", map[string]any{
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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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
		SET title = CASE WHEN $2 IS NULL THEN title ELSE $2 END,
			spec_md = CASE WHEN $3 IS NULL THEN spec_md ELSE $3 END,
			result_md = CASE WHEN $4 IS NULL THEN result_md ELSE $4 END,
			updated_at = NOW()
		WHERE id = $1`,
		taskID, req.Title, req.SpecMD, req.ResultMD); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "strategy": req.Strategy})
}

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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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

func logEvent(ctx context.Context, tx *sql.Tx, taskID int64, eventType, actorType, actorID string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(task_id, event_type, actor_type, actor_id, payload_json)
		VALUES ($1, $2, $3, $4, $5::jsonb)`,
		taskID, eventType, actorType, actorID, string(payloadJSON))
	return err
}

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

func nullableTaskIDEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func lockProjectTopology(ctx context.Context, tx *sql.Tx, projectID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock((71425::bigint << 32) + $1::bigint)`, projectID)
	return err
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/healthz") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
