package enterprise

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditEntry is one audited action.
type AuditEntry struct {
	Time      time.Time `json:"time"`
	User      string    `json:"user"`
	Workspace string    `json:"workspace,omitempty"`
	Cluster   string    `json:"cluster,omitempty"`
	Action    string    `json:"action"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	RemoteIP  string    `json:"remoteIp,omitempty"`
}

// Audit is an append-only JSON-lines audit log.
type Audit struct {
	path string
	mu   sync.Mutex
}

// NewAudit opens the log in dir.
func NewAudit(dir string) (*Audit, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Audit{path: filepath.Join(dir, "audit.jsonl")}, nil
}

// Record appends an entry.
func (a *Audit) Record(e AuditEntry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, _ := json.Marshal(e)
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// Query returns the most recent entries matching q (substring over all fields).
func (a *Audit) Query(q string, limit int) ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.Open(a.path)
	if os.IsNotExist(err) {
		return []AuditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	lq := strings.ToLower(q)
	for sc.Scan() {
		line := sc.Text()
		if lq != "" && !strings.Contains(strings.ToLower(line), lq) {
			continue
		}
		var e AuditEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	// newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Path returns the log file (for export / SIEM shipping).
func (a *Audit) Path() string { return a.path }
