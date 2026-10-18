package enterprise

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

// Roles in ascending privilege.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

var roleRank = map[string]int{RoleViewer: 1, RoleEditor: 2, RoleAdmin: 3, RoleOwner: 4}

// Member is a workspace member (user or group).
type Member struct {
	Subject string `json:"subject"` // email, username or group:<name>
	Role    string `json:"role"`
}

// Workspace groups clusters, members and shared settings for a team.
type Workspace struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	ClusterIDs  []string          `json:"clusterIds"`
	Members     []Member          `json:"members"`
	Settings    map[string]string `json:"settings,omitempty"`
	Namespaces  []string          `json:"namespaces,omitempty"` // optional namespace restriction
	Created     time.Time         `json:"created"`
}

// Workspaces manages team workspaces and enforces application level RBAC.
type Workspaces struct {
	Store *store.Store
	mu    sync.Mutex
}

func (w *Workspaces) all() []Workspace {
	var out []Workspace
	_ = w.Store.Load("workspaces", &out)
	return out
}

// List returns all workspaces.
func (w *Workspaces) List() []Workspace {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.all()
	if out == nil {
		out = []Workspace{}
	}
	return out
}

// Get returns a workspace.
func (w *Workspaces) Get(id string) (*Workspace, error) {
	for _, ws := range w.List() {
		if ws.ID == id {
			return &ws, nil
		}
	}
	return nil, fmt.Errorf("workspace %q not found", id)
}

// Save creates or updates a workspace.
func (w *Workspaces) Save(ws Workspace) (Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	all := w.all()
	if ws.ID == "" {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		ws.ID = hex.EncodeToString(b)
		ws.Created = time.Now()
		all = append(all, ws)
	} else {
		found := false
		for i := range all {
			if all[i].ID == ws.ID {
				ws.Created = all[i].Created
				all[i] = ws
				found = true
			}
		}
		if !found {
			all = append(all, ws)
		}
	}
	for _, m := range ws.Members {
		if _, ok := roleRank[m.Role]; !ok {
			return ws, fmt.Errorf("invalid role %q", m.Role)
		}
	}
	return ws, w.Store.Save("workspaces", all)
}

// Delete removes a workspace.
func (w *Workspaces) Delete(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var kept []Workspace
	for _, ws := range w.all() {
		if ws.ID != id {
			kept = append(kept, ws)
		}
	}
	return w.Store.Save("workspaces", kept)
}

// RoleOf returns the effective role of a user in a workspace.
func (ws *Workspace) RoleOf(u User) string {
	best := ""
	for _, m := range ws.Members {
		match := m.Subject == u.Email || m.Subject == u.Name || m.Subject == u.ID
		for _, g := range u.Groups {
			if m.Subject == "group:"+g {
				match = true
			}
		}
		if match && roleRank[m.Role] > roleRank[best] {
			best = m.Role
		}
	}
	return best
}

// Authorize checks that the user has at least the required role for the
// cluster. Clusters not in any workspace are unrestricted. When RBAC is
// disabled everything is allowed.
func (w *Workspaces) Authorize(enabled bool, u User, clusterID, required string) error {
	if !enabled || clusterID == "" {
		return nil
	}
	inAny := false
	for _, ws := range w.List() {
		for _, c := range ws.ClusterIDs {
			if c != clusterID {
				continue
			}
			inAny = true
			if roleRank[ws.RoleOf(u)] >= roleRank[required] {
				return nil
			}
		}
	}
	if !inAny {
		return nil
	}
	return fmt.Errorf("forbidden: %s requires role %q in a workspace containing this cluster", u.Name, required)
}
