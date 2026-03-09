package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type server struct {
	db                       *sql.DB
	treeGuardMode            string
	auth                     *authStore
	reconcileInterval        time.Duration
	defaultHeartbeatStaleSec int
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
	ParentTaskID         *int64   `json:"parent_task_id"`
	Title                string   `json:"title"`
	SpecMD               string   `json:"spec_md"`
	MaxAttempts          *int     `json:"max_attempts"`
	RequiredCapabilities []string `json:"required_capabilities"`
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
	ProjectID    int64    `json:"project_id"`
	AgentID      string   `json:"agent_id"`
	LeaseSeconds int64    `json:"lease_seconds"`
	Capabilities []string `json:"capabilities"`
}

type heartbeatReq struct {
	AgentID      string `json:"agent_id"`
	LeaseSeconds int64  `json:"lease_seconds"`
	ClaimToken   string `json:"claim_token"`
}

type releaseReq struct {
	AgentID    string `json:"agent_id"`
	ClaimToken string `json:"claim_token"`
	ToStatus   string `json:"to_status"`
}

type completeReq struct {
	AgentID              string          `json:"agent_id"`
	ClaimToken           string          `json:"claim_token"`
	ResultMD             string          `json:"result_md"`
	ResultJSON           json.RawMessage `json:"result_payload"`
	ResultPayloadVersion string          `json:"result_payload_version"`
}

type failReq struct {
	AgentID              string          `json:"agent_id"`
	ClaimToken           string          `json:"claim_token"`
	Reason               string          `json:"reason"`
	ResultMD             string          `json:"result_md"`
	ResultJSON           json.RawMessage `json:"result_payload"`
	ResultPayloadVersion string          `json:"result_payload_version"`
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

type updateTaskCapabilitiesReq struct {
	RequiredCapabilities []string `json:"required_capabilities"`
}

type linkTaskGitRefReq struct {
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"base_commit"`
	CommitSHA  string `json:"commit_sha"`
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
	RecentRuns   []taskRunSummary    `json:"recent_runs"`
	GitRefs      []taskGitRefSummary `json:"git_refs"`
}

type taskRunSummary struct {
	ID         int64           `json:"id"`
	AgentID    string          `json:"agent_id"`
	AttemptNo  int             `json:"attempt_no"`
	Status     string          `json:"status"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	ResultJSON json.RawMessage `json:"result_payload"`
}

type taskGitRefSummary struct {
	ID         int64     `json:"id"`
	Repo       string    `json:"repo"`
	Branch     string    `json:"branch"`
	BaseCommit string    `json:"base_commit"`
	CommitSHA  string    `json:"commit_sha"`
	CreatedAt  time.Time `json:"created_at"`
}

type taskEventSummary struct {
	ID        int64           `json:"id"`
	TaskID    int64           `json:"task_id"`
	ProjectID int64           `json:"project_id"`
	EventType string          `json:"event_type"`
	ActorType string          `json:"actor_type"`
	ActorID   string          `json:"actor_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type staleClaimReconcileReport struct {
	ProjectID          int64   `json:"project_id"`
	ReleasedClaimIDs   []int64 `json:"released_claim_ids"`
	ResetTaskIDs       []int64 `json:"reset_task_ids"`
	ReleasedTaskRunIDs []int64 `json:"released_task_run_ids"`
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

	authMode := getenv("AUTH_MODE", "off")
	authTokens := getenv("AUTH_TOKENS", "")
	auth, err := loadAuthStore(authMode, authTokens)
	if err != nil {
		return err
	}

	reconcileIntervalSec := 30
	if parsed, parseErr := strconv.Atoi(strings.TrimSpace(getenv("RECONCILE_INTERVAL_SECONDS", "30"))); parseErr == nil && parsed >= 0 {
		reconcileIntervalSec = parsed
	}
	heartbeatStaleSec := 300
	if parsed, parseErr := strconv.Atoi(strings.TrimSpace(getenv("HEARTBEAT_STALE_SECONDS", "300"))); parseErr == nil && parsed > 0 {
		heartbeatStaleSec = parsed
	}

	s := &server{
		db:                       db,
		treeGuardMode:            treeGuardMode,
		auth:                     auth,
		reconcileInterval:        time.Duration(reconcileIntervalSec) * time.Second,
		defaultHeartbeatStaleSec: heartbeatStaleSec,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/projects", s.projects)
	mux.HandleFunc("/projects/", s.projectsSubrouter)
	mux.HandleFunc("/tasks/reorder", s.reorderTasks)
	mux.HandleFunc("/tasks/", s.tasksSubrouter)
	mux.HandleFunc("/agents/claim-next", s.claimNext)

	h := withAuth(auth, withJSON(withCORS(mux)))
	if s.reconcileInterval > 0 {
		go s.runClaimReconciler(s.reconcileInterval)
		log.Printf("claim reconciler enabled: interval=%s", s.reconcileInterval)
	} else {
		log.Printf("claim reconciler disabled")
	}
	log.Printf("runtime alerts heartbeat stale threshold=%ds", s.defaultHeartbeatStaleSec)
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
		if !s.requireScope(w, r, "project:read") {
			return
		}
		s.listProjects(w, r)
	case http.MethodPost:
		if !s.requireScope(w, r, "task:admin") {
			return
		}
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
		if !s.requireProjectAccess(w, r, projectID, "task:admin", true) {
			return
		}
		s.updateProject(w, r, projectID)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if !s.requireProjectAccess(w, r, projectID, "task:admin", true) {
			return
		}
		s.deleteProject(w, r, projectID)
		return
	}
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	if parts[1] == "tasks" {
		if len(parts) == 2 && r.Method == http.MethodPost {
			if !s.requireProjectAccess(w, r, projectID, "task:admin", true) {
				return
			}
			s.createTask(w, r, projectID)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			if !s.requireProjectAccess(w, r, projectID, "project:read", false) {
				return
			}
			s.listProjectTasks(w, r, projectID)
			return
		}
		if len(parts) == 3 && parts[2] == "tree" && r.Method == http.MethodGet {
			if !s.requireProjectAccess(w, r, projectID, "project:read", false) {
				return
			}
			s.getProjectTaskTree(w, r, projectID)
			return
		}
		if len(parts) == 3 && parts[2] == "validate" && r.Method == http.MethodPost {
			if !s.requireProjectAccess(w, r, projectID, "task:admin", true) {
				return
			}
			s.validateProjectTree(w, r, projectID)
			return
		}
	}
	if len(parts) == 3 && parts[1] == "claims" && parts[2] == "reconcile" && r.Method == http.MethodPost {
		if !s.requireProjectAccess(w, r, projectID, "task:admin", true) {
			return
		}
		s.reconcileProjectClaims(w, r, projectID)
		return
	}
	if len(parts) == 2 && parts[1] == "runtime-alerts" && r.Method == http.MethodGet {
		if !s.requireProjectAccess(w, r, projectID, "project:read", false) {
			return
		}
		s.getProjectRuntimeAlerts(w, r, projectID)
		return
	}
	if len(parts) == 2 && parts[1] == "events" && r.Method == http.MethodGet {
		if !s.requireProjectAccess(w, r, projectID, "project:read", false) {
			return
		}
		s.listProjectEvents(w, r, projectID)
		return
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
				if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
					return
				}
				s.addDependency(w, r, taskID)
				return
			}
			if r.Method == http.MethodGet {
				if _, ok := s.requireTaskAccess(w, r, taskID, "project:read", false); !ok {
					return
				}
				s.listDependencies(w, r, taskID)
				return
			}
		}
		if len(parts) == 3 && r.Method == http.MethodDelete {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
				return
			}
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
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:update", false); !ok {
				return
			}
			s.updateTaskStatus(w, r, taskID)
			return
		}
	case "context":
		if r.Method == http.MethodGet {
			if _, ok := s.requireTaskAccess(w, r, taskID, "project:read", false); !ok {
				return
			}
			s.getTaskContext(w, r, taskID)
			return
		}
	case "events":
		if r.Method == http.MethodGet {
			if _, ok := s.requireTaskAccess(w, r, taskID, "project:read", false); !ok {
				return
			}
			s.listTaskEvents(w, r, taskID)
			return
		}
	case "heartbeat":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:claim", false); !ok {
				return
			}
			s.heartbeatTaskClaim(w, r, taskID)
			return
		}
	case "release":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:claim", false); !ok {
				return
			}
			s.releaseTaskClaim(w, r, taskID)
			return
		}
	case "complete":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:complete", false); !ok {
				return
			}
			s.completeTask(w, r, taskID)
			return
		}
	case "fail":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:complete", false); !ok {
				return
			}
			s.failTask(w, r, taskID)
			return
		}
	case "move":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
				return
			}
			s.moveTask(w, r, taskID)
			return
		}
	case "content":
		if r.Method == http.MethodPatch {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:update", false); !ok {
				return
			}
			s.updateTaskContent(w, r, taskID)
			return
		}
	case "delete":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
				return
			}
			s.deleteTask(w, r, taskID)
			return
		}
	case "execution-policy":
		if r.Method == http.MethodPatch {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
				return
			}
			s.updateTaskExecutionPolicy(w, r, taskID)
			return
		}
	case "capabilities":
		if r.Method == http.MethodGet {
			if _, ok := s.requireTaskAccess(w, r, taskID, "project:read", false); !ok {
				return
			}
			s.getTaskCapabilities(w, r, taskID)
			return
		}
		if r.Method == http.MethodPatch {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:admin", true); !ok {
				return
			}
			s.updateTaskCapabilities(w, r, taskID)
			return
		}
	case "git-link":
		if r.Method == http.MethodPost {
			if _, ok := s.requireTaskAccess(w, r, taskID, "task:update", false); !ok {
				return
			}
			s.linkTaskGitRef(w, r, taskID)
			return
		}
	}

	http.NotFound(w, r)
}
