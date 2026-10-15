// Package ai implements the built-in AI assistant: an agentic loop on the
// Claude API (via the official Anthropic Go SDK) with Kubernetes tools, MCP
// server tools and custom plugin tools. An OpenAI-compatible local endpoint
// (e.g. Ollama or vLLM) is supported for air-gapped installations.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is the default Claude model.
const DefaultModel = "claude-opus-5-5"

// Settings configures the assistant.
type Settings struct {
	Provider    string `json:"provider"` // anthropic | local
	APIKey      string `json:"apiKey,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"` // Anthropic gateway override (proxies, air-gapped gateways)
	Model       string `json:"model"`
	Effort      string `json:"effort"` // low | medium | high | xhigh | max
	LocalURL    string `json:"localUrl,omitempty"`
	LocalModel  string `json:"localModel,omitempty"`
	AllowWrites bool   `json:"allowWrites"`
	MaxSteps    int    `json:"maxSteps"`
}

// Tool is a function the model may call.
type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
	Write       bool // mutates the cluster; only offered when writes are allowed
	Run         func(ctx context.Context, input json.RawMessage) (string, error)
}

// Message is a chat history entry exchanged with the UI.
type Message struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

// Request asks the assistant something.
type Request struct {
	Task      string    `json:"task"`
	Prompt    string    `json:"prompt"`
	Context   string    `json:"context"`
	ClusterID string    `json:"clusterId"`
	Namespace string    `json:"namespace"`
	History   []Message `json:"history"`
}

// Step is a progress event (tool call / result) streamed to the UI.
type Step struct {
	Type   string `json:"type"` // tool_call | tool_result | text | error
	Tool   string `json:"tool,omitempty"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Text   string `json:"text,omitempty"`
}

// Answer is the final response.
type Answer struct {
	Text  string `json:"text"`
	Steps []Step `json:"steps"`
	Model string `json:"model"`
}

// Assistant runs requests.
type Assistant struct {
	Settings func() Settings
	// ToolsFor returns the tools available for a cluster (built-in + MCP + plugins).
	ToolsFor func(clusterID, namespace string) []Tool
}

func buildUserText(r Request) string {
	var sb strings.Builder
	if t := Tasks[r.Task]; t != "" {
		sb.WriteString(t)
		sb.WriteString("\n\n")
	}
	if r.ClusterID != "" {
		fmt.Fprintf(&sb, "Active cluster id: %s. Current namespace: %s.\n\n", r.ClusterID, orDefault(r.Namespace, "(all)"))
	}
	if r.Prompt != "" {
		sb.WriteString(r.Prompt)
		sb.WriteString("\n\n")
	}
	if r.Context != "" {
		sb.WriteString("<context>\n")
		sb.WriteString(r.Context)
		sb.WriteString("\n</context>")
	}
	return strings.TrimSpace(sb.String())
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Ask runs the request to completion, invoking onStep for progress.
func (a *Assistant) Ask(ctx context.Context, r Request, onStep func(Step)) (*Answer, error) {
	st := a.Settings()
	if onStep == nil {
		onStep = func(Step) {}
	}
	var tools []Tool
	if a.ToolsFor != nil && r.ClusterID != "" {
		for _, t := range a.ToolsFor(r.ClusterID, r.Namespace) {
			if t.Write && !st.AllowWrites {
				continue
			}
			tools = append(tools, t)
		}
	}
	if st.Provider == "local" {
		return a.askLocal(ctx, st, r, tools, onStep)
	}
	return a.askClaude(ctx, st, r, tools, onStep)
}

func (a *Assistant) askClaude(ctx context.Context, st Settings, r Request, tools []Tool, onStep func(Step)) (*Answer, error) {
	opts := []option.RequestOption{}
	if st.APIKey != "" {
		opts = append(opts, option.WithAPIKey(st.APIKey))
	}
	if st.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(st.BaseURL))
	}
	client := anthropic.NewClient(opts...)
	model := orDefault(st.Model, DefaultModel)
	maxSteps := st.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}

	var toolParams []anthropic.BetaToolUnionParam
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Name] = t
		tp := anthropic.BetaToolUnionParamOfTool(anthropic.BetaToolInputSchemaParam{Properties: t.Properties, Required: t.Required}, t.Name)
		tp.OfTool.Description = anthropic.String(t.Description)
		toolParams = append(toolParams, tp)
	}

	var msgs []anthropic.BetaMessageParam
	for _, h := range r.History {
		if h.Content == "" {
			continue
		}
		if h.Role == "assistant" {
			msgs = append(msgs, anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(h.Content)}})
		} else {
			msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(h.Content)))
		}
	}
	msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(buildUserText(r))))

	ans := &Answer{Model: model}
	for step := 0; step < maxSteps; step++ {
		params := anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: 16000,
			System:    []anthropic.BetaTextBlockParam{{Text: SystemPrompt}},
			Messages:  msgs,
			Tools:     toolParams,
			// Re-serve policy declines on a fallback model chosen by the API.
			Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
			Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		}
		if st.Effort != "" {
			params.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(st.Effort)}
		}
		resp, err := client.Beta.Messages.New(ctx, params)
		if err != nil {
			var apiErr *anthropic.Error
			if errors.As(err, &apiErr) {
				switch apiErr.StatusCode {
				case 401:
					return nil, fmt.Errorf("Claude API key is missing or invalid (Settings → AI)")
				case 429:
					return nil, fmt.Errorf("Claude API rate limit reached, try again shortly")
				}
			}
			return nil, err
		}
		if resp.StopReason == anthropic.BetaStopReasonRefusal {
			ans.Text = "The request was declined by the model's safety system."
			return ans, nil
		}
		msgs = append(msgs, resp.ToParam())
		var results []anthropic.BetaContentBlockParamUnion
		var text strings.Builder
		for _, block := range resp.Content {
			switch b := block.AsAny().(type) {
			case anthropic.BetaTextBlock:
				text.WriteString(b.Text)
			case anthropic.BetaToolUseBlock:
				input := json.RawMessage(b.JSON.Input.Raw())
				s := Step{Type: "tool_call", Tool: b.Name, Input: string(input)}
				ans.Steps = append(ans.Steps, s)
				onStep(s)
				out, isErr := runTool(ctx, byName, b.Name, input)
				rs := Step{Type: "tool_result", Tool: b.Name, Output: truncate(out, 2000)}
				ans.Steps = append(ans.Steps, rs)
				onStep(rs)
				results = append(results, anthropic.NewBetaToolResultBlock(b.ID, truncate(out, 60000), isErr))
			}
		}
		if text.Len() > 0 && resp.StopReason == anthropic.BetaStopReasonToolUse {
			onStep(Step{Type: "text", Text: text.String()})
		}
		if resp.StopReason != anthropic.BetaStopReasonToolUse || len(results) == 0 {
			ans.Text = text.String()
			if resp.StopReason == anthropic.BetaStopReasonMaxTokens {
				ans.Text += "\n\n_(response truncated)_"
			}
			return ans, nil
		}
		msgs = append(msgs, anthropic.NewBetaUserMessage(results...))
	}
	ans.Text = "Stopped after reaching the maximum number of tool steps."
	return ans, nil
}

func runTool(ctx context.Context, tools map[string]Tool, name string, input json.RawMessage) (string, bool) {
	t, ok := tools[name]
	if !ok {
		return "unknown tool " + name, true
	}
	if len(input) == 0 || !json.Valid(input) {
		return "INVALID_JSON: tool input was not valid JSON", true
	}
	out, err := t.Run(ctx, input)
	if err != nil {
		return "error: " + err.Error(), true
	}
	return out, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n…[truncated %d bytes]", len(s)-n)
}

// askLocal talks to an OpenAI-compatible chat completions endpoint (Ollama,
// vLLM, LM Studio) for air-gapped environments. Tools use function calling.
func (a *Assistant) askLocal(ctx context.Context, st Settings, r Request, tools []Tool, onStep func(Step)) (*Answer, error) {
	if st.LocalURL == "" {
		return nil, fmt.Errorf("local model URL not configured (Settings → AI)")
	}
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	type msg struct {
		Role       string     `json:"role"`
		Content    string     `json:"content"`
		ToolCalls  []toolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	msgs := []msg{{Role: "system", Content: SystemPrompt}}
	for _, h := range r.History {
		msgs = append(msgs, msg{Role: h.Role, Content: h.Content})
	}
	msgs = append(msgs, msg{Role: "user", Content: buildUserText(r)})
	byName := map[string]Tool{}
	var tdefs []map[string]any
	for _, t := range tools {
		byName[t.Name] = t
		tdefs = append(tdefs, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description,
			"parameters": map[string]any{"type": "object", "properties": t.Properties, "required": t.Required},
		}})
	}
	ans := &Answer{Model: st.LocalModel}
	for step := 0; step < 15; step++ {
		body := map[string]any{"model": st.LocalModel, "messages": msgs}
		if len(tdefs) > 0 {
			body["tools"] = tdefs
		}
		b, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(st.LocalURL, "/")+"/chat/completions", bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if st.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+st.APIKey)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("local model: %s: %s", resp.Status, raw)
		}
		var out struct {
			Choices []struct {
				Message msg `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
			return nil, fmt.Errorf("unexpected local model response")
		}
		m := out.Choices[0].Message
		msgs = append(msgs, m)
		if len(m.ToolCalls) == 0 {
			ans.Text = m.Content
			return ans, nil
		}
		for _, tc := range m.ToolCalls {
			s := Step{Type: "tool_call", Tool: tc.Function.Name, Input: tc.Function.Arguments}
			ans.Steps = append(ans.Steps, s)
			onStep(s)
			res, _ := runTool(ctx, byName, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			rs := Step{Type: "tool_result", Tool: tc.Function.Name, Output: truncate(res, 2000)}
			ans.Steps = append(ans.Steps, rs)
			onStep(rs)
			msgs = append(msgs, msg{Role: "tool", ToolCallID: tc.ID, Content: truncate(res, 30000)})
		}
	}
	ans.Text = "Stopped after reaching the maximum number of tool steps."
	return ans, nil
}
