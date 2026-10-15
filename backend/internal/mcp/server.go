package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
)

// ServerTool is a tool exposed by Serve.
type ServerTool struct {
	Tool
	Handler func(ctx context.Context, args json.RawMessage) (string, error)
}

// Serve runs an MCP server over the given reader/writer (stdio).
func Serve(ctx context.Context, in io.Reader, out io.Writer, name, version string, tools []ServerTool) error {
	byName := map[string]ServerTool{}
	var defs []Tool
	for _, t := range tools {
		byName[t.Name] = t
		defs = append(defs, t.Tool)
	}
	enc := json.NewEncoder(out)
	reply := func(id *int64, result any, rerr *rpcError) {
		m := rpcMessage{JSONRPC: "2.0", ID: id, Error: rerr}
		if result != nil {
			m.Result, _ = json.Marshal(result)
		}
		_ = enc.Encode(m)
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var m rpcMessage
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.ID == nil {
			continue // notification
		}
		switch m.Method {
		case "initialize":
			reply(m.ID, map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": name, "version": version},
			}, nil)
		case "ping":
			reply(m.ID, map[string]any{}, nil)
		case "tools/list":
			reply(m.ID, map[string]any{"tools": defs}, nil)
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(m.Params, &p)
			t, ok := byName[p.Name]
			if !ok {
				reply(m.ID, nil, &rpcError{Code: -32602, Message: "unknown tool " + p.Name})
				continue
			}
			text, err := t.Handler(ctx, p.Arguments)
			isErr := false
			if err != nil {
				text, isErr = err.Error(), true
			}
			reply(m.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}, nil)
		default:
			reply(m.ID, nil, &rpcError{Code: -32601, Message: "method not found"})
		}
	}
	return sc.Err()
}
