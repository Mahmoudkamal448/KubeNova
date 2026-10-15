package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestToolLoop drives the agent loop against a fake Messages API: the first
// response requests a tool call, the second returns the final answer.
func TestToolLoop(t *testing.T) {
	var calls atomic.Int32
	var secondBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			if !strings.Contains(string(b), `"name":"list_resources"`) {
				t.Errorf("tools not sent: %s", b)
			}
			_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"tool_use",
				"content":[{"type":"text","text":"Checking pods."},{"type":"tool_use","id":"toolu_1","name":"list_resources","input":{"kind":"pods"}}],
				"usage":{"input_tokens":10,"output_tokens":5}}`)
			return
		}
		secondBody = string(b)
		_, _ = io.WriteString(w, `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn",
			"content":[{"type":"text","text":"Pod web-1 is CrashLoopBackOff."}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer srv.Close()

	var got json.RawMessage
	a := &Assistant{
		Settings: func() Settings { return Settings{Provider: "anthropic", APIKey: "test", BaseURL: srv.URL} },
		ToolsFor: func(string, string) []Tool {
			return []Tool{{Name: "list_resources", Description: "list", Properties: map[string]any{"kind": prop("string", "kind")},
				Run: func(_ context.Context, in json.RawMessage) (string, error) {
					got = in
					return "default/web-1 status=CrashLoopBackOff", nil
				}}}
		},
	}
	var steps []Step
	ans, err := a.Ask(context.Background(), Request{Task: "troubleshoot-pod", ClusterID: "c1"}, func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "Pod web-1 is CrashLoopBackOff." {
		t.Fatalf("unexpected answer %q", ans.Text)
	}
	if !strings.Contains(string(got), `"pods"`) {
		t.Fatalf("tool input not forwarded: %s", got)
	}
	if !strings.Contains(secondBody, `"tool_use_id":"toolu_1"`) || !strings.Contains(secondBody, "CrashLoopBackOff") {
		t.Fatalf("tool result not returned to the model: %s", secondBody)
	}
	if len(steps) < 2 || steps[0].Type != "tool_call" {
		t.Fatalf("progress steps missing: %+v", steps)
	}
}

func TestWriteToolsHiddenByDefault(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"x","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	a := &Assistant{
		Settings: func() Settings { return Settings{APIKey: "k", BaseURL: srv.URL} },
		ToolsFor: func(string, string) []Tool {
			return []Tool{{Name: "apply_yaml", Write: true, Properties: map[string]any{}}, {Name: "get_events", Properties: map[string]any{}}}
		},
	}
	if _, err := a.Ask(context.Background(), Request{Prompt: "x", ClusterID: "c"}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent, "apply_yaml") || !strings.Contains(sent, "get_events") {
		t.Fatalf("write tool exposure wrong: %s", sent)
	}
}
