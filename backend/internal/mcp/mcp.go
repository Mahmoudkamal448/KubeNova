// Package mcp implements the Model Context Protocol over stdio: a client used
// to attach external MCP servers to the AI assistant, and a server that
// exposes KubeNova's Kubernetes tools to other MCP clients (Claude Code,
// Claude Desktop, IDEs).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision implemented.
const ProtocolVersion = "2025-06-18"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is an MCP tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ServerConfig configures an external stdio MCP server.
type ServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// Client is a connection to one stdio MCP server.
type Client struct {
	Config  ServerConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan rpcMessage
	Tools   []Tool
	closed  chan struct{}
}

// Connect launches the server process and performs the MCP handshake.
func Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{Config: cfg, cmd: cmd, stdin: stdin, pending: map[int64]chan rpcMessage{}, closed: make(chan struct{})}
	go c.readLoop(stdout)
	initParams := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "kubenova", "version": "1.0.0"},
	}
	if _, err := c.call(ctx, "initialize", initParams); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize %s: %w", cfg.Name, err)
	}
	_ = c.notify("notifications/initialized", nil)
	res, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		c.Close()
		return nil, err
	}
	var tl struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(res, &tl); err != nil {
		c.Close()
		return nil, err
	}
	c.Tools = tl.Tools
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	defer close(c.closed)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var m rpcMessage
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m.ID == nil || m.Method != "" {
			continue
		}
		c.mu.Lock()
		ch := c.pending[*m.ID]
		delete(c.pending, *m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

func (c *Client) send(m rpcMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *Client) notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	return c.send(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	raw, _ := json.Marshal(params)
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, fmt.Errorf("%s", m.Error.Message)
		}
		return m.Result, nil
	case <-c.closed:
		return nil, fmt.Errorf("mcp server %s exited", c.Config.Name)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CallTool invokes a tool and returns its text content.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", true, err
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return string(res), false, nil
	}
	out := ""
	for _, c := range r.Content {
		if c.Type == "text" {
			out += c.Text
		}
	}
	return out, r.IsError, nil
}

// Close terminates the server.
func (c *Client) Close() {
	c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

// Manager keeps connections to configured MCP servers.
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client
	errors  map[string]string
}

// NewManager creates an empty manager.
func NewManager() *Manager {
	return &Manager{clients: map[string]*Client{}, errors: map[string]string{}}
}

// Sync connects enabled servers and disconnects removed/disabled ones.
func (m *Manager) Sync(ctx context.Context, cfgs []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]ServerConfig{}
	for _, c := range cfgs {
		if c.Enabled {
			want[c.Name] = c
		}
	}
	for name, c := range m.clients {
		if _, ok := want[name]; !ok {
			c.Close()
			delete(m.clients, name)
		}
	}
	m.errors = map[string]string{}
	for name, cfg := range want {
		if _, ok := m.clients[name]; ok {
			continue
		}
		c, err := Connect(ctx, cfg)
		if err != nil {
			m.errors[name] = err.Error()
			continue
		}
		m.clients[name] = c
	}
}

// Status describes a server connection.
type Status struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Tools     []Tool `json:"tools"`
	Error     string `json:"error,omitempty"`
}

// Status reports all servers.
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Status
	for name, c := range m.clients {
		out = append(out, Status{Name: name, Connected: true, Tools: c.Tools})
	}
	for name, e := range m.errors {
		out = append(out, Status{Name: name, Error: e})
	}
	return out
}

// Clients returns the connected clients.
func (m *Manager) Clients() []*Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Client
	for _, c := range m.clients {
		out = append(out, c)
	}
	return out
}
