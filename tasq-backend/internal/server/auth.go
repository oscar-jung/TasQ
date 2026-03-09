package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type authStore struct {
	mode   string
	tokens map[string]actor
}

type actor struct {
	Type           string
	ID             string
	Scopes         map[string]struct{}
	ProjectAll     bool
	ProjectScopeID map[int64]struct{}
}

type tokenConfig struct {
	Token     string   `json:"token"`
	ActorType string   `json:"actor_type"`
	ActorID   string   `json:"actor_id"`
	Scopes    []string `json:"scopes"`
	Projects  []string `json:"projects"`
}

type actorContextKey struct{}

func loadAuthStore(mode, raw string) (*authStore, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "off"
	}
	if mode != "off" && mode != "optional" && mode != "required" {
		return nil, fmt.Errorf("invalid AUTH_MODE: %s", mode)
	}

	store := &authStore{mode: mode, tokens: map[string]actor{}}
	if strings.TrimSpace(raw) == "" {
		return store, nil
	}

	var cfg []tokenConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("invalid AUTH_TOKENS json: %w", err)
	}

	for _, c := range cfg {
		if strings.TrimSpace(c.Token) == "" || strings.TrimSpace(c.ActorType) == "" || strings.TrimSpace(c.ActorID) == "" {
			return nil, fmt.Errorf("invalid token config: token/actor_type/actor_id required")
		}
		a := actor{
			Type:           c.ActorType,
			ID:             c.ActorID,
			Scopes:         map[string]struct{}{},
			ProjectScopeID: map[int64]struct{}{},
		}
		for _, s := range c.Scopes {
			if strings.TrimSpace(s) != "" {
				a.Scopes[s] = struct{}{}
			}
		}
		for _, p := range c.Projects {
			if p == "*" {
				a.ProjectAll = true
				continue
			}
			id, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid project scope %q in token config", p)
			}
			a.ProjectScopeID[id] = struct{}{}
		}
		store.tokens[c.Token] = a
	}
	return store, nil
}

func withAuth(store *authStore, next http.Handler) http.Handler {
	if store == nil || store.mode == "off" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorContextKey{}, legacyActor())))
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if authz == "" {
			if store.mode == "required" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorContextKey{}, legacyActor())))
			return
		}
		if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(authz[len("Bearer "):])
		a, ok := store.tokens[token]
		if !ok {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorContextKey{}, a)))
	})
}

func legacyActor() actor {
	return actor{
		Type:       "human",
		ID:         "local-user",
		ProjectAll: true,
		Scopes: map[string]struct{}{
			"project:read":  {},
			"task:claim":    {},
			"task:update":   {},
			"task:complete": {},
			"task:admin":    {},
		},
		ProjectScopeID: map[int64]struct{}{},
	}
}

func actorFromContext(ctx context.Context) actor {
	v := ctx.Value(actorContextKey{})
	a, ok := v.(actor)
	if !ok {
		return legacyActor()
	}
	return a
}

func (a actor) hasScope(scope string) bool {
	_, ok := a.Scopes[scope]
	return ok
}

func (a actor) allowsProject(projectID int64) bool {
	if a.ProjectAll {
		return true
	}
	_, ok := a.ProjectScopeID[projectID]
	return ok
}

func (s *server) requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	a := actorFromContext(r.Context())
	if !a.hasScope(scope) {
		http.Error(w, "forbidden: missing scope", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) requireProjectAccess(w http.ResponseWriter, r *http.Request, projectID int64, scope string, humanOnly bool) bool {
	a := actorFromContext(r.Context())
	if !a.hasScope(scope) {
		http.Error(w, "forbidden: missing scope", http.StatusForbidden)
		return false
	}
	if !a.allowsProject(projectID) {
		http.Error(w, "forbidden: project scope denied", http.StatusForbidden)
		return false
	}
	if humanOnly && a.Type != "human" {
		http.Error(w, "forbidden: human actor required", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) requireTaskAccess(w http.ResponseWriter, r *http.Request, taskID int64, scope string, humanOnly bool) (int64, bool) {
	projectID, err := s.fetchProjectIDByTaskID(r.Context(), taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return 0, false
	}
	if !s.requireProjectAccess(w, r, projectID, scope, humanOnly) {
		return 0, false
	}
	return projectID, true
}

func (s *server) requireAgentIdentity(w http.ResponseWriter, r *http.Request, requestedAgentID string) bool {
	a := actorFromContext(r.Context())
	if a.Type != "agent" {
		return true
	}
	if strings.TrimSpace(requestedAgentID) == "" || requestedAgentID != a.ID {
		http.Error(w, "forbidden: agent_id mismatch", http.StatusForbidden)
		return false
	}
	return true
}

func actorForEvent(r *http.Request) (string, string) {
	a := actorFromContext(r.Context())
	if strings.TrimSpace(a.Type) == "" || strings.TrimSpace(a.ID) == "" {
		return "human", "local-user"
	}
	return a.Type, a.ID
}
