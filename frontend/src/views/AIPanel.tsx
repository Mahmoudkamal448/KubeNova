import { useEffect, useRef, useState } from "react";
import { Send, Sparkles, Trash2, X, Wrench } from "lucide-react";
import { api, wsUrl } from "../api";
import { useStore } from "../store";
import { Markdown } from "../components/ui";
import { CreateResource } from "./CreateResource";

interface Msg { role: "user" | "assistant"; content: string; steps?: { type: string; tool?: string; input?: string; output?: string; text?: string }[]; pending?: boolean; task?: string }

const quick: [string, string, string][] = [
  ["troubleshoot-pod", "Troubleshoot failing pods", "Find failing pods in the current namespace and diagnose them."],
  ["analyze-events", "Analyze warning events", "Analyze recent Warning events in the current namespace."],
  ["cost", "Cost optimization", "Review resource requests vs usage and suggest savings."],
  ["security", "Security review", "Review the current namespace for security risks."],
  ["kubectl", "Generate kubectl", ""],
  ["generate-yaml", "Generate YAML", ""],
  ["explain-alert", "Explain firing alerts", "Explain currently firing Prometheus alerts and how to fix them."],
  ["flux-failure", "Explain Flux failures", "Find Flux objects that are not Ready and explain why."],
  ["argocd-failure", "Explain Argo CD failures", "Find Argo CD applications that are OutOfSync or Degraded and explain why."],
];

export function AIPanel() {
  const s = useStore();
  const [msgs, setMsgs] = useState<Msg[]>(() => {
    try { return JSON.parse(sessionStorage.getItem("kubenova.ai") || "[]"); } catch { return []; }
  });
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [yamlDraft, setYamlDraft] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const consumed = useRef<unknown>(null);

  useEffect(() => { sessionStorage.setItem("kubenova.ai", JSON.stringify(msgs.slice(-40))); endRef.current?.scrollIntoView({ behavior: "smooth" }); }, [msgs]);

  const send = (task: string, prompt: string, context = "") => {
    if (busy) return;
    const history = msgs.filter((m) => !m.pending).map((m) => ({ role: m.role, content: m.content })).slice(-12);
    const label = prompt || (task !== "chat" ? task.replace(/-/g, " ") : "");
    setMsgs((m) => [...m, { role: "user", content: label + (context ? `\n\n_(with ${context.length} chars of context)_` : ""), task }, { role: "assistant", content: "", steps: [], pending: true }]);
    setBusy(true);
    const ws = new WebSocket(wsUrl("/ai/stream"));
    ws.onopen = () => ws.send(JSON.stringify({ task, prompt, context, clusterId: s.clusterId, namespace: s.namespace, history }));
    const update = (fn: (m: Msg) => Msg) => setMsgs((all) => { const c = [...all]; c[c.length - 1] = fn(c[c.length - 1]); return c; });
    ws.onmessage = (e) => {
      const d = JSON.parse(e.data);
      if (d.step) update((m) => ({ ...m, steps: [...(m.steps || []), d.step] }));
      if (d.answer) { update((m) => ({ ...m, content: d.answer.text, pending: false })); setBusy(false); ws.close(); }
      if (d.error) { update((m) => ({ ...m, content: `**Error:** ${d.error}`, pending: false })); setBusy(false); ws.close(); }
    };
    ws.onerror = () => { update((m) => ({ ...m, content: "**Error:** connection failed", pending: false })); setBusy(false); };
  };

  useEffect(() => {
    if (s.aiSeed && consumed.current !== s.aiSeed) {
      consumed.current = s.aiSeed;
      if (s.aiSeed.prompt === "" && !s.aiSeed.context && (s.aiSeed.task === "generate-yaml" || s.aiSeed.task === "kubectl")) {
        setInput("");
        (document.getElementById("ai-input") as HTMLTextAreaElement)?.focus();
        (window as any).__aiTask = s.aiSeed.task;
        return;
      }
      send(s.aiSeed.task, s.aiSeed.prompt || "", s.aiSeed.context);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s.aiSeed]);

  const model = s.settings?.ai?.provider === "local" ? s.settings?.ai?.localModel : s.settings?.ai?.model;
  return (
    <div className="drawer" style={{ width: "min(560px, 45vw)", zIndex: 30 }}>
      <div className="drawer-head">
        <Sparkles size={16} color="var(--accent2)" /><b className="grow">AI Assistant</b>
        <span className="muted small">{model}{s.settings?.ai?.allowWrites ? " · writes enabled" : " · read-only"}</span>
        <button className="btn ghost sm" title="Clear" onClick={() => setMsgs([])}><Trash2 size={14} /></button>
        <button className="btn ghost sm" onClick={s.closeAI}><X size={15} /></button>
      </div>
      <div className="grow" style={{ overflow: "auto", padding: "4px 14px" }}>
        {!msgs.length && (
          <div className="col" style={{ marginTop: 12 }}>
            <div className="muted">Ask anything about your clusters. The assistant inspects resources, events, logs and metrics with tools{s.clusterId ? "" : " once a cluster is selected"}, and can use connected MCP servers and plugin tools.</div>
            <div className="row wrap">{quick.map(([t, l, p]) => <button key={t} className="btn sm" onClick={() => (p ? send(t, p) : ((window as any).__aiTask = t, document.getElementById("ai-input")?.focus()))}>{l}</button>)}</div>
          </div>
        )}
        {msgs.map((m, i) => (
          <div key={i} className={`ai-msg ${m.role}`}>
            {m.steps?.filter((st) => st.type === "tool_call").map((st, j) => <div key={j} className="step"><Wrench size={10} /> {st.tool}({(st.input || "").slice(0, 120)})</div>)}
            {m.pending && <div className="muted small">thinking{m.steps?.length ? ` · ${m.steps.filter((x) => x.type === "tool_call").length} tool calls` : ""}…</div>}
            {m.role === "assistant" ? <Markdown text={m.content} onApplyYaml={(y) => setYamlDraft(y)} /> : <div style={{ whiteSpace: "pre-wrap" }}>{m.content}</div>}
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <div style={{ padding: 10, borderTop: "1px solid var(--border)" }}>
        <div className="row">
          <textarea id="ai-input" rows={3} className="grow" style={{ fontFamily: "inherit", fontSize: 13 }} placeholder="Ask about your cluster, paste YAML/logs, or describe what to generate… (Enter to send)"
            value={input} onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (input.trim()) { send((window as any).__aiTask || "chat", input); (window as any).__aiTask = ""; setInput(""); } } }} />
          <button className="btn ai" disabled={busy || !input.trim()} onClick={() => { send((window as any).__aiTask || "chat", input); (window as any).__aiTask = ""; setInput(""); }}><Send size={14} /></button>
        </div>
      </div>
      {yamlDraft !== null && <CreateResource initialYaml={yamlDraft} onClose={() => setYamlDraft(null)} />}
    </div>
  );
}

export async function aiDashboard(prompt: string, clusterId: string): Promise<any> {
  const a = await api.post("/ai/ask", { task: "dashboard", prompt, clusterId });
  const m = (a.text as string).match(/\{[\s\S]*\}/);
  if (!m) throw new Error("AI did not return a dashboard");
  return JSON.parse(m[0]);
}
