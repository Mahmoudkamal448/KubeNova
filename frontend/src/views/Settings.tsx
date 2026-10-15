import { useEffect, useState } from "react";
import { Plus, Save, Trash2 } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Field, useAsync } from "../components/ui";

export function SettingsView() {
  const s = useStore();
  const [st, setSt] = useState<any>(null);
  const info = useAsync(() => api.get("/info"), []);
  const mcp = useAsync(() => api.get("/mcp"), []);
  useEffect(() => { api.get("/settings").then(setSt); }, []);
  if (!st) return <div className="page">Loading…</div>;
  const set = (k: string, v: any) => setSt({ ...st, [k]: v });
  const setAI = (k: string, v: any) => setSt({ ...st, ai: { ...st.ai, [k]: v } });
  const save = async () => { if (await act(() => api.put("/settings", st), "Settings saved")) { s.loadSettings(); setTimeout(mcp.reload, 1500); } };
  return (
    <div className="page">
      <h1 className="row"><span className="grow">Settings</span><button className="btn primary" onClick={save}><Save size={13} />Save</button></h1>
      <div className="grid g2">
        <div className="card col"><h3>AI assistant</h3>
          <Field label="Provider"><select value={st.ai.provider} onChange={(e) => setAI("provider", e.target.value)}><option value="anthropic">Claude (Anthropic API)</option><option value="local">Local / self-hosted model (OpenAI-compatible: Ollama, vLLM)</option></select></Field>
          {st.ai.provider !== "local" ? <>
            <Field label="Model"><select value={st.ai.model} onChange={(e) => setAI("model", e.target.value)}>{["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"].map((m) => <option key={m}>{m}</option>)}</select></Field>
            <Field label="Effort"><select value={st.ai.effort} onChange={(e) => setAI("effort", e.target.value)}>{["low", "medium", "high", "xhigh", "max"].map((m) => <option key={m}>{m}</option>)}</select></Field>
            <Field label="API key (or set ANTHROPIC_API_KEY)"><input type="password" value={st.ai.apiKey || ""} onChange={(e) => setAI("apiKey", e.target.value)} /></Field>
            <Field label="Gateway base URL (optional)"><input value={st.ai.baseUrl || ""} onChange={(e) => setAI("baseUrl", e.target.value)} placeholder="https://api.anthropic.com" /></Field>
          </> : <>
            <Field label="Endpoint (…/v1)"><input value={st.ai.localUrl || ""} onChange={(e) => setAI("localUrl", e.target.value)} placeholder="http://localhost:11434/v1" /></Field>
            <Field label="Model"><input value={st.ai.localModel || ""} onChange={(e) => setAI("localModel", e.target.value)} placeholder="llama3.1:70b" /></Field>
          </>}
          <Field label="Max tool steps"><input type="number" value={st.ai.maxSteps} onChange={(e) => setAI("maxSteps", +e.target.value)} /></Field>
          <label className="inline"><input type="checkbox" checked={st.ai.allowWrites} onChange={(e) => setAI("allowWrites", e.target.checked)} />Allow the assistant to change clusters (apply, scale, restart)</label>
        </div>
        <div className="card col"><h3>MCP servers</h3>
          <div className="muted small">Connect Model Context Protocol servers over stdio; their tools become available to the assistant. KubeNova itself can be used as an MCP server: <span className="mono">kubenova-server --mcp --cluster &lt;context&gt;</span>.</div>
          {(st.mcpServers || []).map((m: any, i: number) => (
            <div key={i} className="row">
              <input value={m.name} placeholder="name" style={{ width: 110 }} onChange={(e) => set("mcpServers", st.mcpServers.map((x: any, j: number) => (j === i ? { ...x, name: e.target.value } : x)))} />
              <input className="grow mono" value={[m.command, ...(m.args || [])].join(" ")} placeholder="npx -y @modelcontextprotocol/server-github" onChange={(e) => { const [command, ...args] = e.target.value.split(" "); set("mcpServers", st.mcpServers.map((x: any, j: number) => (j === i ? { ...x, command, args } : x))); }} />
              <input type="checkbox" checked={m.enabled} onChange={(e) => set("mcpServers", st.mcpServers.map((x: any, j: number) => (j === i ? { ...x, enabled: e.target.checked } : x)))} />
              <button className="btn ghost sm" onClick={() => set("mcpServers", st.mcpServers.filter((_: any, j: number) => j !== i))}><Trash2 size={12} /></button>
            </div>))}
          <button className="btn sm" style={{ alignSelf: "start" }} onClick={() => set("mcpServers", [...(st.mcpServers || []), { name: "", command: "", args: [], enabled: true }])}><Plus size={12} />Add server</button>
          {(mcp.data || []).map((m: any) => <div key={m.name} className="small">{m.name}: {m.connected ? <Badge kind="ok">{m.tools.length} tools</Badge> : <Badge kind="error">{m.error}</Badge>}</div>)}
        </div>
        <div className="card col"><h3>Network, air-gapped & compliance</h3>
          <Field label="HTTP(S) proxy for clusters, tools and integrations"><input value={st.proxy || ""} onChange={(e) => set("proxy", e.target.value)} placeholder="http://proxy.corp:3128" /></Field>
          <label className="inline"><input type="checkbox" checked={st.airGapped} onChange={(e) => set("airGapped", e.target.checked)} />Air-gapped mode (no internet: local AI model, no marketplace, bundled CLIs)</label>
          <Field label="Marketplace index URL"><input value={st.marketplaceUrl || ""} onChange={(e) => set("marketplaceUrl", e.target.value)} /></Field>
          <div className="small">FIPS: <Badge kind={info.data?.license?.fips ? "ok" : ""}>{info.data?.license?.fips ? "FIPS 140-3 module active" : "standard crypto"}</Badge></div>
        </div>
        <div className="card col"><h3>Performance & general</h3>
          <Field label="Theme"><select value={s.theme} onChange={(e) => s.setTheme(e.target.value)}><option value="dark">Dark</option><option value="light">Light</option><option value="system">System</option></select></Field>
          <Field label="List page size (lazy loading)"><input type="number" value={st.pageSize} onChange={(e) => set("pageSize", +e.target.value)} /></Field>
          <Field label="Background health check interval (s)"><input type="number" value={st.healthIntervalSeconds} onChange={(e) => set("healthIntervalSeconds", +e.target.value)} /></Field>
          <Field label="Health check workers (parallel clusters)"><input type="number" value={st.healthWorkers} onChange={(e) => set("healthWorkers", +e.target.value)} /></Field>
          <Field label="Terminal shell"><input value={st.shell || ""} onChange={(e) => set("shell", e.target.value)} placeholder="default login shell" /></Field>
          <Field label="Working directory"><input value={st.workdir || ""} onChange={(e) => set("workdir", e.target.value)} /></Field>
        </div>
        <div className="card col"><h3>Bundled tools</h3>
          <div className="row wrap">{Object.entries(info.data?.tools || {}).map(([k, v]) => <Badge key={k} kind={v ? "ok" : ""}>{k}{v ? "" : " (missing)"}</Badge>)}</div>
          <div className="muted small">Place binaries in {info.data?.dataDir}/bin to bundle them (used first, before PATH).</div>
        </div>
        <div className="card col"><h3>About</h3><div>KubeNova {info.data?.version}</div><div className="muted small">Data directory: {info.data?.dataDir}</div></div>
      </div>
    </div>
  );
}
