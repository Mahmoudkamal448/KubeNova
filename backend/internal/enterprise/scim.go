package enterprise

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/mahmoudkamal448/kubenova/backend/internal/store"
)

// SCIM implements a minimal SCIM 2.0 provisioning endpoint (Users and
// Groups) so identity providers (Okta, Entra ID, OneLogin) can provision
// workspace members.
type SCIM struct {
	Store *store.Store
	Token func() string
	mu    sync.Mutex
}

type scimData struct {
	Users  map[string]map[string]any `json:"users"`
	Groups map[string]map[string]any `json:"groups"`
}

func (s *SCIM) load() scimData {
	d := scimData{Users: map[string]map[string]any{}, Groups: map[string]map[string]any{}}
	_ = s.Store.Load("scim", &d)
	if d.Users == nil {
		d.Users = map[string]map[string]any{}
	}
	if d.Groups == nil {
		d.Groups = map[string]map[string]any{}
	}
	return d
}

// Users returns provisioned users (for the workspace member picker).
func (s *SCIM) Users() []map[string]any {
	var out []map[string]any
	for _, u := range s.load().Users {
		out = append(out, u)
	}
	return out
}

func scimErr(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"}, "status": code, "detail": detail})
}

// ServeHTTP handles /scim/v2/{Users,Groups}[/{id}].
func (s *SCIM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := s.Token()
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(tok)) != 1 {
		scimErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/scim/v2"), "/")
	parts := strings.SplitN(path, "/", 2)
	var kind string
	switch parts[0] {
	case "Users":
		kind = "users"
	case "Groups":
		kind = "groups"
	case "ServiceProviderConfig":
		_ = json.NewEncoder(w).Encode(map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
			"patch": map[string]bool{"supported": true}, "filter": map[string]any{"supported": true, "maxResults": 1000}})
		return
	default:
		scimErr(w, http.StatusNotFound, "unknown resource")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.load()
	coll := d.Users
	schema := "urn:ietf:params:scim:schemas:core:2.0:User"
	if kind == "groups" {
		coll = d.Groups
		schema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	}
	id := ""
	if len(parts) == 2 {
		id = parts[1]
	}
	w.Header().Set("Content-Type", "application/scim+json")
	switch {
	case r.Method == http.MethodGet && id == "":
		filter := r.URL.Query().Get("filter") // supports: attr eq "value"
		var res []map[string]any
		for _, v := range coll {
			if filter != "" {
				f := strings.SplitN(filter, " eq ", 2)
				if len(f) == 2 {
					if val, _ := v[strings.TrimSpace(f[0])].(string); !strings.EqualFold(val, strings.Trim(f[1], `" `)) {
						continue
					}
				}
			}
			res = append(res, v)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"}, "totalResults": len(res), "Resources": res, "startIndex": 1, "itemsPerPage": len(res)})
	case r.Method == http.MethodGet:
		v, ok := coll[id]
		if !ok {
			scimErr(w, 404, "not found")
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	case r.Method == http.MethodPost:
		var v map[string]any
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			scimErr(w, 400, err.Error())
			return
		}
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		v["id"] = hex.EncodeToString(b)
		v["schemas"] = []string{schema}
		coll[v["id"].(string)] = v
		_ = s.Store.Save("scim", d)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(v)
	case r.Method == http.MethodPut:
		var v map[string]any
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			scimErr(w, 400, err.Error())
			return
		}
		if _, ok := coll[id]; !ok {
			scimErr(w, 404, "not found")
			return
		}
		v["id"] = id
		v["schemas"] = []string{schema}
		coll[id] = v
		_ = s.Store.Save("scim", d)
		_ = json.NewEncoder(w).Encode(v)
	case r.Method == http.MethodPatch:
		v, ok := coll[id]
		if !ok {
			scimErr(w, 404, "not found")
			return
		}
		var p struct {
			Operations []struct {
				Op    string `json:"op"`
				Path  string `json:"path"`
				Value any    `json:"value"`
			} `json:"Operations"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			scimErr(w, 400, err.Error())
			return
		}
		for _, op := range p.Operations {
			switch strings.ToLower(op.Op) {
			case "replace", "add":
				if op.Path == "" {
					if m, ok := op.Value.(map[string]any); ok {
						for k, val := range m {
							v[k] = val
						}
					}
				} else if strings.ToLower(op.Op) == "add" && op.Path == "members" {
					cur, _ := v["members"].([]any)
					if add, ok := op.Value.([]any); ok {
						v["members"] = append(cur, add...)
					}
				} else {
					v[op.Path] = op.Value
				}
			case "remove":
				delete(v, op.Path)
			}
		}
		_ = s.Store.Save("scim", d)
		_ = json.NewEncoder(w).Encode(v)
	case r.Method == http.MethodDelete:
		delete(coll, id)
		_ = s.Store.Save("scim", d)
		w.WriteHeader(http.StatusNoContent)
	default:
		scimErr(w, 405, "method not allowed")
	}
}
