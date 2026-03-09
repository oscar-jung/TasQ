package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type server struct {
	db            *sql.DB
	treeGuardMode string
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
	MaxAttempts  int        `json:"max_attempts"`
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
	MaxAttempts  *int   `json:"max_attempts"`
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

type updateTaskExecutionPolicyReq struct {
	MaxAttempts int `json:"max_attempts"`
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

// Run starts the HTTP server and wires shared middleware and routes.
func Run() error {
	dbURL := getenv("DATABASE_URL", "postgres://tasq:tasq@db:5432/tasq?sslmode=disable")
	port := getenv("PORT", "8080")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}

	treeGuardMode := strings.ToLower(strings.TrimSpace(getenv("TREE_GUARD_MODE", "off")))
	if treeGuardMode != "off" && treeGuardMode != "validate" && treeGuardMode != "cleanse" {
		log.Printf("invalid TREE_GUARD_MODE=%q, fallback to off", treeGuardMode)
		treeGuardMode = "off"
	}

	s := &server{db: db, treeGuardMode: treeGuardMode}
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
		return err
	}
	return nil
}

// healthz serves liveness checks for containers and orchestrators.
func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// projects handles /projects with GET(list) and POST(create).
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

// projectsSubrouter dispatches /projects/:id and /projects/:id/tasks* endpoints.
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
		if len(parts) == 3 && parts[2] == "validate" && r.Method == http.MethodPost {
			s.validateProjectTree(w, r, projectID)
			return
		}
	}

	http.NotFound(w, r)
}

// tasksSubrouter dispatches /tasks/:id/* task lifecycle endpoints.
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
	case "execution-policy":
		if r.Method == http.MethodPatch {
			s.updateTaskExecutionPolicy(w, r, taskID)
			return
		}
	}

	http.NotFound(w, r)
}
