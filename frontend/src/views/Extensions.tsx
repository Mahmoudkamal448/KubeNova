import { useEffect, useState } from "react";
import { Download, Palette, Puzzle, Store, Trash2, Upload, Keyboard } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Tabs, useAsync } from "../components/ui";
import { PluginPanel } from "../plugins";

export const builtinThemes = [
  { id: "dark", name: "KubeNova Dark", dark: true },
  { id: "light", name: "KubeNova Light", dark: false },
  { id: "system", name: "Follow system", dark: true },
];

/** Registers plugin themes globally so App can apply their colour tokens. */
export function usePluginThemes() {
  const plugins = useAsync(() => api.get("/plugins").catch(() => []), []);
  useEffect(() => {
    const themes: Record<string, any> = {};
    for (const p of plugins.data || []) if (p.enabled) for (const t of p.themes || []) themes[t.id] = t;
    (window as any).__kubenovaThemes = themes;
  }, [plugins.data]);
  return plugins.data || [];
}

export function ExtensionsView() {
  const [tab, setTab] = useState<"installed" | "panels" | "marketplace" | "themes" | "shortcuts" | "sdk">("installed");
  return (
    <div className="page" style={{ padding: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "14px 20px 0" }}><h1><Puzzle size={18} />Extensions</h1></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "installed", label: "Installed" }, { id: "panels", label: "Plugin panels" }, { id: "marketplace", label: "Marketplace" }, { id: "themes", label: "Themes" }, { id: "shortcuts", label: "Keyboard shortcuts" }, { id: "sdk", label: "Plugin SDK" }]} />
      <div className="grow" style={{ padding: 16, overflow: "auto" }}>
        {tab === "installed" && <Installed />}
        {tab === "panels" && <Panels />}
        {tab === "marketplace" && <Marketplace />}
        {tab === "themes" && <Themes />}
        {tab === "shortcuts" && <Shortcuts />}
        {tab === "sdk" && <SDK />}
      </div>
    </div>
  );
}

function Installed() {
  const p = useAsync(() => api.get("/plugins"), []);
  const upload = async (f: File) => { await act(() => api.upload("/plugins/upload", f), `Installed ${f.name}`); p.reload(); };
  return (
    <div className="col">
      <div className="row"><label className="btn primary"><Upload size={13} />Install from .zip<input type="file" accept=".zip" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} /></label></div>
      <ErrorBox error={p.error} />
      {(p.data || []).map((x: any) => (
        <div className="card" key={x.id}>
          <div className="row"><b className="grow">{x.name} <span className="muted small">{x.version} · {x.author}</span></b>
            <Badge kind={x.enabled ? "ok" : ""}>{x.enabled ? "enabled" : "disabled"}</Badge>
            <button className="btn sm" onClick={async () => { await api.post(`/plugins/${x.id}/${x.enabled ? "disable" : "enable"}`); p.reload(); }}>{x.enabled ? "Disable" : "Enable"}</button>
            <button className="btn sm danger" onClick={async () => { await api.del(`/plugins/${x.id}`); p.reload(); }}><Trash2 size={12} /></button></div>
          <div className="small">{x.description}</div>
          <div className="row wrap small" style={{ marginTop: 6 }}>
            {x.panels?.length > 0 && <Badge>{x.panels.length} panels</Badge>}{x.dashboards?.length > 0 && <Badge>{x.dashboards.length} dashboards</Badge>}
            {x.themes?.length > 0 && <Badge>{x.themes.length} themes</Badge>}{x.aiTools?.length > 0 && <Badge kind="info">{x.aiTools.length} AI tools</Badge>}{x.shortcuts?.length > 0 && <Badge>{x.shortcuts.length} shortcuts</Badge>}
          </div>
        </div>
      ))}
      {p.data && !p.data.length && <Empty>No plugins installed.</Empty>}
    </div>
  );
}

function Marketplace() {
  const m = useAsync(() => api.get("/marketplace"), []);
  const [q, setQ] = useState("");
  return (
    <div className="col">
      <input placeholder="Search marketplace…" value={q} onChange={(e) => setQ(e.target.value)} />
      <ErrorBox error={m.error} />
      <div className="grid g3">{(m.data || []).filter((e: any) => !q || `${e.name} ${e.description} ${(e.tags || []).join(" ")}`.toLowerCase().includes(q.toLowerCase())).map((e: any) => (
        <div className="card" key={e.id}><div className="row"><Store size={14} /><b className="grow">{e.name}</b><span className="muted small">★ {e.stars}</span></div>
          <div className="small" style={{ minHeight: 36 }}>{e.description}</div>
          <div className="row wrap">{(e.tags || []).map((t: string) => <span key={t} className="chip">{t}</span>)}</div>
          <button className="btn primary sm" style={{ marginTop: 8 }} onClick={() => act(() => api.post("/marketplace/install", { download: e.download }), `Installed ${e.name}`)}><Download size={12} />Install</button></div>
      ))}</div>
    </div>
  );
}

function Themes() {
  const s = useStore();
  const plugins = usePluginThemes();
  const themes = [...builtinThemes, ...plugins.flatMap((p: any) => (p.enabled ? p.themes || [] : []))];
  return (
    <div className="grid g4">{themes.map((t: any) => (
      <div key={t.id} className="card tile" style={{ borderColor: s.theme === t.id ? "var(--accent)" : undefined }} onClick={() => s.setTheme(t.id)}>
        <div className="row"><Palette size={14} /><b>{t.name}</b>{s.theme === t.id && <Badge kind="ok">active</Badge>}</div>
        <div className="row" style={{ marginTop: 8 }}>{Object.values(t.colors || (t.dark ? { a: "#0b0f17", b: "#1a2233", c: "#4f8cff" } : { a: "#f6f7fb", b: "#ffffff", c: "#2563eb" })).slice(0, 6).map((c: any, i) => <span key={i} style={{ width: 22, height: 22, borderRadius: 5, background: c, border: "1px solid var(--border)" }} />)}</div>
      </div>
    ))}</div>
  );
}

function Shortcuts() {
  const s = useStore();
  const [m, setM] = useState<Record<string, string>>(s.settings?.shortcuts || {});
  const fixed: [string, string][] = [["Command palette", "Ctrl/Cmd + K"], ["AI assistant", "Ctrl/Cmd + I"], ["Terminal", "Ctrl/Cmd + `"], ["Multi-cluster search", "Ctrl/Cmd + Shift + F"], ["Jump to view N", "Ctrl/Cmd + 1…9"], ["k9s-style resource jump", ": (then po, deploy, svc…)"], ["Filter current table", "/"]];
  return (
    <div className="col">
      <table className="tbl"><thead><tr><th>Action</th><th>Keys</th></tr></thead><tbody>{fixed.map(([a, k]) => <tr key={a}><td>{a}</td><td><span className="kbd">{k}</span></td></tr>)}</tbody></table>
      <h2>Custom bindings</h2>
      {Object.entries(m).map(([k, v]) => <div key={k} className="row"><span style={{ width: 160 }}>{k}</span><input value={v} onChange={(e) => setM({ ...m, [k]: e.target.value })} /></div>)}
      <button className="btn primary" style={{ alignSelf: "start" }} onClick={async () => { const cur = await api.get("/settings"); await act(() => api.put("/settings", { ...cur, shortcuts: m }), "Saved"); s.loadSettings(); }}><Keyboard size={13} />Save shortcuts</button>
    </div>
  );
}

function SDK() {
  return (
    <div className="col">
      <div>Plugins are folders (or .zip archives) containing a <code>plugin.json</code> manifest. They can contribute UI panels (ES module entry), declarative PromQL dashboards, colour themes, keyboard shortcuts and custom AI tools (a command reading JSON on stdin, or an HTTP endpoint).</div>
      <pre>{`{
  "id": "acme-tools",
  "name": "ACME platform tools",
  "version": "1.0.0",
  "author": "ACME",
  "description": "Panels, dashboards and AI tools for ACME clusters",
  "entry": "index.js",
  "panels": [{ "id": "ownership", "title": "Ownership", "location": "resource-tab", "kinds": ["Deployment"] }],
  "dashboards": [{ "id": "ingress", "title": "Ingress NGINX", "panels": [
    { "title": "Requests/s", "type": "line", "unit": "rps", "query": "sum(rate(nginx_ingress_controller_requests[5m]))" } ] }],
  "themes": [{ "id": "acme", "name": "ACME", "dark": true, "colors": { "bg": "#0d1117", "accent": "#ff7a00" } }],
  "shortcuts": [{ "keys": "Mod+Shift+O", "command": "acme.ownership" }],
  "aiTools": [{
    "name": "lookup_owner", "description": "Find the owning team of a workload",
    "properties": { "name": { "type": "string" } }, "required": ["name"],
    "command": ["python3", "owner.py"]
  }]
}`}</pre>
      <div>The UI module receives a <code>kubenova</code> object with <code>api</code> (authenticated backend client), <code>cluster</code>, <code>namespace</code> and <code>registerPanel(id, render)</code>. See <code>docs/plugin-sdk.md</code> and <code>examples/plugin-hello</code>.</div>
    </div>
  );
}

function Panels() {
  const s = useStore();
  const p = useAsync(() => api.get("/plugins"), []);
  const panels = (p.data || []).filter((x: any) => x.enabled).flatMap((x: any) => (x.panels || []).filter((pn: any) => pn.location !== "resource-tab").map((pn: any) => ({ ...pn, plugin: x.name })));
  const [sel, setSel] = useState("");
  if (!panels.length) return <Empty>No plugin panels. Plugins can contribute sidebar and dashboard panels.</Empty>;
  const cur = sel || panels[0].id;
  return (
    <div className="col">
      <div className="row wrap">{panels.map((pn: any) => <button key={pn.id} className={`btn sm ${pn.id === cur ? "primary" : ""}`} onClick={() => setSel(pn.id)}>{pn.title} <span className="muted small">· {pn.plugin}</span></button>)}</div>
      <div className="card"><PluginPanel id={cur} ctx={{ clusterId: s.clusterId, namespace: s.namespace }} /></div>
    </div>
  );
}
