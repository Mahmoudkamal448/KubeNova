import { useEffect, useState } from "react";
import { Bell, Plus, RefreshCw, Sparkles, Trash2, Wand2 } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Field, Modal, Tabs, useAsync } from "../components/ui";
import { Gauge, LineChart, promToSeries } from "../components/Charts";
import { bytes, cores, pct } from "../util";
import { aiDashboard } from "./AIPanel";

type Tab = "overview" | "dashboards" | "promql" | "loki" | "alerts" | "traces" | "backends";

export function MonitoringView() {
  const s = useStore();
  const [tab, setTab] = useState<Tab>("overview");
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster.</Empty></div>;
  return (
    <div className="page" style={{ padding: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "14px 20px 0" }}><h1>Monitoring</h1></div>
      <Tabs value={tab} onChange={setTab} tabs={[
        { id: "overview", label: "Cluster overview" }, { id: "dashboards", label: "Live dashboards" }, { id: "promql", label: "PromQL" },
        { id: "loki", label: "Loki logs" }, { id: "alerts", label: "Alertmanager" }, { id: "traces", label: "Tempo traces" }, { id: "backends", label: "Data sources" },
      ]} />
      <div style={{ padding: 16, overflow: "auto" }} className="grow">
        {tab === "overview" && <Overview />}
        {tab === "dashboards" && <Dashboards />}
        {tab === "promql" && <PromQL />}
        {tab === "loki" && <Loki />}
        {tab === "alerts" && <Alerts />}
        {tab === "traces" && <Traces />}
        {tab === "backends" && <Backends />}
      </div>
    </div>
  );
}

function Overview() {
  const s = useStore();
  const o = useAsync(() => api.get(`/clusters/${s.clusterId}/metrics/overview`), [s.clusterId]);
  useEffect(() => { const id = setInterval(o.reload, 15000); return () => clearInterval(id); }, [o.reload]);
  const d = o.data;
  if (!d) return <ErrorBox error={o.error} />;
  const ns = Object.values(d.namespaces || {}).sort((a: any, b: any) => b.cpuMilli - a.cpuMilli) as any[];
  return (
    <div className="col" style={{ gap: 14 }}>
      {d.source === "unavailable" && <div className="card" style={{ borderColor: "var(--warn)" }}>metrics-server is not available — showing capacity and requests only. Connect Prometheus in Data sources for history.</div>}
      <div className="grid g4">
        <div className="card"><Gauge value={pct(d.cpuUsageMilli, d.cpuCapacityMilli)} label="CPU usage" sub={`${cores(d.cpuUsageMilli)} / ${cores(d.cpuCapacityMilli)}`} /></div>
        <div className="card"><Gauge value={pct(d.cpuRequestsMilli, d.cpuCapacityMilli)} label="CPU requests" sub={cores(d.cpuRequestsMilli)} /></div>
        <div className="card"><Gauge value={pct(d.memoryUsageBytes, d.memoryCapacityBytes)} label="Memory usage" sub={`${bytes(d.memoryUsageBytes)} / ${bytes(d.memoryCapacityBytes)}`} /></div>
        <div className="card"><Gauge value={pct(d.memoryRequestsBytes, d.memoryCapacityBytes)} label="Memory requests" sub={bytes(d.memoryRequestsBytes)} /></div>
      </div>
      <div className="grid g2">
        <div className="card"><h3>Node metrics</h3>
          <table className="tbl small"><thead><tr><th>Node</th><th>CPU</th><th>Memory</th></tr></thead>
            <tbody>{(d.nodes || []).map((n: any) => <tr key={n.name}><td>{n.name}</td><td>{cores(n.cpuMilli)}</td><td>{bytes(n.memoryBytes)}</td></tr>)}</tbody></table>
        </div>
        <div className="card"><h3>Namespace metrics</h3>
          <table className="tbl small"><thead><tr><th>Namespace</th><th>CPU</th><th>Memory</th></tr></thead>
            <tbody>{ns.map((n) => <tr key={n.name}><td>{n.name}</td><td>{cores(n.cpuMilli)}</td><td>{bytes(n.memoryBytes)}</td></tr>)}</tbody></table>
        </div>
      </div>
      <PodTop />
    </div>
  );
}

function PodTop() {
  const s = useStore();
  const p = useAsync(() => api.get(`/clusters/${s.clusterId}/metrics/pods?namespace=${s.namespace}`), [s.clusterId, s.namespace]);
  const rows = [...(p.data || [])].sort((a: any, b: any) => b.cpuMilli - a.cpuMilli).slice(0, 50);
  return (
    <div className="card"><h3>Pod metrics (top 50 by CPU{s.namespace ? ` in ${s.namespace}` : ""})</h3>
      <table className="tbl small"><thead><tr><th>Pod</th><th>Namespace</th><th>CPU</th><th>Memory</th><th>Containers</th></tr></thead>
        <tbody>{rows.map((r: any) => <tr key={r.namespace + r.name}><td>{r.name}</td><td>{r.namespace}</td><td>{cores(r.cpuMilli)}</td><td>{bytes(r.memoryBytes)}</td><td className="muted">{(r.containers || []).map((c: any) => `${c.name}:${c.cpuMilli}m`).join(" ")}</td></tr>)}</tbody></table>
    </div>
  );
}

interface Panel { title: string; type: string; query: string; unit?: string }
interface Dashboard { id?: string; title: string; panels: Panel[] }

const builtin: Dashboard[] = [
  { id: "cluster", title: "Cluster", panels: [
    { title: "CPU (cores)", type: "line", query: "cluster_cpu", unit: "cores" }, { title: "Memory", type: "line", query: "cluster_memory", unit: "bytes" },
    { title: "Network receive", type: "line", query: "cluster_network_rx", unit: "bytes" }, { title: "Network transmit", type: "line", query: "cluster_network_tx", unit: "bytes" },
    { title: "Filesystem usage", type: "line", query: "cluster_fs", unit: "bytes" }] },
  { id: "namespaces", title: "Namespaces", panels: [{ title: "CPU by namespace", type: "line", query: "namespace_cpu", unit: "cores" }, { title: "Memory by namespace", type: "line", query: "namespace_memory", unit: "bytes" }] },
  { id: "nodes", title: "Nodes", panels: [{ title: "Node CPU utilisation", type: "line", query: "node_cpu", unit: "percent" }, { title: "Node memory utilisation", type: "line", query: "node_memory", unit: "percent" }, { title: "Node filesystem utilisation", type: "line", query: "node_fs", unit: "percent" }] },
  { id: "pods", title: "Pods (current namespace)", panels: [
    { title: "Pod CPU", type: "line", query: "pod_cpu", unit: "cores" }, { title: "Pod memory", type: "line", query: "pod_memory", unit: "bytes" },
    { title: "Pod network rx", type: "line", query: "pod_network_rx", unit: "bytes" }, { title: "Pod network tx", type: "line", query: "pod_network_tx", unit: "bytes" }, { title: "Pod filesystem", type: "line", query: "pod_fs", unit: "bytes" }] },
];

function Dashboards() {
  const s = useStore();
  const queries = useAsync(() => api.get("/metrics/queries"), []);
  const custom = useAsync(() => api.get<Dashboard[]>("/dashboards"), []);
  const [sel, setSel] = useState("cluster");
  const [range, setRange] = useState(3600);
  const [editing, setEditing] = useState<Dashboard | null>(null);
  const [aiPrompt, setAiPrompt] = useState("");
  const [aiBusy, setAiBusy] = useState(false);
  const all = [...builtin, ...(custom.data || []).map((d, i) => ({ ...d, id: d.id || "custom" + i }))];
  const dash = all.find((d) => d.id === sel) || builtin[0];
  const resolve = (q: string) => (queries.data?.[q] || q).replace(/\$namespace/g, s.namespace || ".*").replace(/\$pod/g, ".*");
  const saveDash = async (d: Dashboard) => {
    const st = await api.get("/settings");
    const list = (st.dashboards || []).filter((x: Dashboard) => x.id !== d.id);
    await act(() => api.put("/settings", { ...st, dashboards: [...list, { ...d, id: d.id || "d" + Date.now() }] }), "Dashboard saved");
    custom.reload();
  };
  return (
    <div className="col">
      <div className="row wrap">
        <select value={sel} onChange={(e) => setSel(e.target.value)}>{all.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</select>
        <select value={range} onChange={(e) => setRange(+e.target.value)}>{[[900, "15m"], [3600, "1h"], [21600, "6h"], [86400, "24h"], [604800, "7d"]].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        <button className="btn" onClick={() => setEditing({ title: "New dashboard", panels: [{ title: "Panel", type: "line", query: "up", unit: "" }] })}><Plus size={13} />Custom dashboard</button>
        {!(dash.id || "").match(/^(cluster|namespaces|nodes|pods)$/) && <button className="btn" onClick={() => setEditing(dash)}>Edit</button>}
        <div className="grow" />
        <input placeholder="Describe a dashboard for AI to build…" value={aiPrompt} onChange={(e) => setAiPrompt(e.target.value)} style={{ width: 320 }} />
        <button className="btn ai" disabled={aiBusy || !aiPrompt} onClick={async () => { setAiBusy(true); const d = await act(() => aiDashboard(aiPrompt, s.clusterId)); setAiBusy(false); if (d) setEditing(d); }}><Wand2 size={13} />{aiBusy ? "Generating…" : "AI-generate"}</button>
      </div>
      <div className="grid g2">{dash.panels.map((p, i) => <PromPanel key={`${dash.id}-${i}-${range}`} panel={p} query={resolve(p.query)} range={range} />)}</div>
      {editing && <DashboardEditor d={editing} onClose={() => setEditing(null)} onSave={(d) => { saveDash(d); setEditing(null); }} />}
    </div>
  );
}

function PromPanel({ panel, query, range }: { panel: Panel; query: string; range: number }) {
  const s = useStore();
  const [data, setData] = useState<any>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    const load = () => api.post(`/clusters/${s.clusterId}/metrics/query`, { query, rangeSeconds: panel.type === "stat" ? 0 : range }).then(setData).catch((e) => setErr(e.message));
    load();
    const id = setInterval(load, 30000);
    return () => clearInterval(id);
  }, [query, range, s.clusterId, panel.type]);
  const series = promToSeries(data);
  return (
    <div className="card">
      <h3 className="row"><span className="grow">{panel.title}</span><button className="btn ghost sm" title="Explain" onClick={() => s.openAI({ task: "explain-promql", context: query })}><Sparkles size={12} /></button></h3>
      {err ? <div className="small" style={{ color: "var(--err)" }}>{err}</div> : panel.type === "stat" ? <div className="stat">{series[0]?.points[0]?.[1]?.toFixed(2) ?? "-"}</div> : <LineChart series={series} unit={panel.unit} />}
    </div>
  );
}

function DashboardEditor({ d, onClose, onSave }: { d: Dashboard; onClose: () => void; onSave: (d: Dashboard) => void }) {
  const [x, setX] = useState<Dashboard>(JSON.parse(JSON.stringify(d)));
  const upd = (i: number, k: keyof Panel, v: string) => setX({ ...x, panels: x.panels.map((p, j) => (j === i ? { ...p, [k]: v } : p)) });
  return (
    <Modal title="Dashboard" onClose={onClose} footer={<button className="btn primary" onClick={() => onSave(x)}>Save</button>}>
      <Field label="Title"><input value={x.title} onChange={(e) => setX({ ...x, title: e.target.value })} /></Field>
      {x.panels.map((p, i) => (
        <div key={i} className="row" style={{ marginTop: 8 }}>
          <input value={p.title} onChange={(e) => upd(i, "title", e.target.value)} style={{ width: 160 }} />
          <select value={p.type} onChange={(e) => upd(i, "type", e.target.value)}><option>line</option><option>stat</option></select>
          <select value={p.unit || ""} onChange={(e) => upd(i, "unit", e.target.value)}>{["", "cores", "bytes", "percent", "rps"].map((u) => <option key={u} value={u}>{u || "unit"}</option>)}</select>
          <input className="grow mono" value={p.query} onChange={(e) => upd(i, "query", e.target.value)} />
          <button className="btn ghost sm" onClick={() => setX({ ...x, panels: x.panels.filter((_, j) => j !== i) })}><Trash2 size={12} /></button>
        </div>
      ))}
      <button className="btn" style={{ marginTop: 8 }} onClick={() => setX({ ...x, panels: [...x.panels, { title: "Panel", type: "line", query: "", unit: "" }] })}><Plus size={13} />Panel</button>
    </Modal>
  );
}

function PromQL() {
  const s = useStore();
  const [q, setQ] = useState('sum by (namespace) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))');
  const [range, setRange] = useState(3600);
  const [res, setRes] = useState<any>(null);
  const [err, setErr] = useState("");
  const run = () => api.post(`/clusters/${s.clusterId}/metrics/query`, { query: q, rangeSeconds: range }).then((r) => { setRes(r); setErr(""); }).catch((e) => setErr(e.message));
  return (
    <div className="col">
      <textarea rows={3} value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === "Enter" && (e.ctrlKey || e.metaKey) && run()} />
      <div className="row"><select value={range} onChange={(e) => setRange(+e.target.value)}>{[[0, "instant"], [3600, "1h"], [21600, "6h"], [86400, "24h"]].map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        <button className="btn primary" onClick={run}>Run (Ctrl+Enter)</button>
        <button className="btn ai" onClick={() => s.openAI({ task: "explain-promql", context: q })}><Sparkles size={13} />Explain</button></div>
      <ErrorBox error={err} />
      {res && <div className="card"><LineChart series={promToSeries(res)} height={260} /></div>}
    </div>
  );
}

function Loki() {
  const s = useStore();
  const [q, setQ] = useState(`{namespace="${s.namespace || "default"}"} |= "error"`);
  const [res, setRes] = useState<any>(null);
  const [err, setErr] = useState("");
  const lines: [string, string, string][] = (res?.data?.result || []).flatMap((r: any) => r.values.map((v: [string, string]) => [v[0], JSON.stringify(r.stream), v[1]])).sort((a: any, b: any) => (a[0] < b[0] ? 1 : -1)).slice(0, 1000);
  return (
    <div className="col">
      <textarea rows={2} value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="row">
        <button className="btn primary" onClick={() => api.post(`/clusters/${s.clusterId}/loki/query`, { query: q }).then((r) => { setRes(r); setErr(""); }).catch((e) => setErr(e.message))}>Run</button>
        <button className="btn ai" onClick={() => s.openAI({ task: "explain-logql", context: q })}><Sparkles size={13} />Explain query</button>
        {lines.length > 0 && <button className="btn ai" onClick={() => s.openAI({ task: "explain-logs", context: lines.slice(0, 300).map((l) => l[2]).join("\n") })}><Sparkles size={13} />Explain results</button>}
      </div>
      <ErrorBox error={err} />
      <div className="logview" style={{ maxHeight: "60vh" }}>{lines.map((l, i) => <div className="ln" key={i}><span className="src">{new Date(+l[0] / 1e6).toLocaleTimeString()}</span>{l[2]}</div>)}</div>
    </div>
  );
}

function Alerts() {
  const s = useStore();
  const a = useAsync(() => api.get(`/clusters/${s.clusterId}/alerts`), [s.clusterId]);
  return (
    <div className="col">
      <div className="row"><button className="btn" onClick={a.reload}><RefreshCw size={13} /></button></div>
      <ErrorBox error={a.error} />
      {(a.data || []).map((al: any, i: number) => (
        <div className="card" key={i}>
          <div className="row"><Bell size={14} /><b className="grow">{al.labels?.alertname}</b><Badge kind={al.labels?.severity === "critical" ? "error" : "warn"}>{al.labels?.severity || "none"}</Badge><Badge>{al.status?.state}</Badge>
            <button className="btn ai sm" onClick={() => s.openAI({ task: "explain-alert", context: JSON.stringify(al, null, 1) })}><Sparkles size={12} />Explain</button></div>
          <div className="small">{al.annotations?.summary || al.annotations?.description || al.annotations?.message}</div>
          <div className="row wrap small" style={{ marginTop: 4 }}>{Object.entries(al.labels || {}).map(([k, v]) => <span className="chip" key={k}>{k}={String(v)}</span>)}</div>
        </div>
      ))}
      {a.data && !a.data.length && <Empty>No active alerts.</Empty>}
    </div>
  );
}

function Traces() {
  const s = useStore();
  const [id, setId] = useState("");
  const [t, setT] = useState<any>(null);
  const [err, setErr] = useState("");
  const spans: any[] = (t?.batches || []).flatMap((b: any) => (b.scopeSpans || b.instrumentationLibrarySpans || []).flatMap((ss: any) => (ss.spans || []).map((sp: any) => ({ ...sp, service: b.resource?.attributes?.find((a: any) => a.key === "service.name")?.value?.stringValue }))));
  const t0 = Math.min(...spans.map((x) => +x.startTimeUnixNano)), t1 = Math.max(...spans.map((x) => +x.endTimeUnixNano));
  return (
    <div className="col">
      <div className="row"><input className="grow mono" placeholder="Trace ID" value={id} onChange={(e) => setId(e.target.value)} />
        <button className="btn primary" onClick={() => api.get(`/clusters/${s.clusterId}/traces/${id}`).then((r) => { setT(r); setErr(""); }).catch((e) => setErr(e.message))}>Load</button></div>
      <ErrorBox error={err} />
      {spans.map((sp, i) => (
        <div key={i} className="row small" style={{ gap: 8 }}>
          <span style={{ width: 260, overflow: "hidden", textOverflow: "ellipsis" }}>{sp.service} · {sp.name}</span>
          <div className="grow bar" style={{ height: 10 }}><div style={{ marginLeft: `${((+sp.startTimeUnixNano - t0) / (t1 - t0 || 1)) * 100}%`, width: `${Math.max(0.5, ((+sp.endTimeUnixNano - +sp.startTimeUnixNano) / (t1 - t0 || 1)) * 100)}%` }} /></div>
          <span className="muted">{((+sp.endTimeUnixNano - +sp.startTimeUnixNano) / 1e6).toFixed(1)}ms</span>
        </div>
      ))}
    </div>
  );
}

const kinds = ["prometheus", "mimir", "victoriametrics", "loki", "tempo", "alertmanager", "grafana"];

function Backends() {
  const s = useStore();
  const eps = useAsync(() => api.get(`/clusters/${s.clusterId}/observability`), [s.clusterId]);
  const [edit, setEdit] = useState<any>(null);
  return (
    <div className="col">
      <div className="row"><button className="btn primary" onClick={async () => { await act(() => api.post(`/clusters/${s.clusterId}/observability/detect`), "Detection finished"); eps.reload(); }}>Auto-detect in cluster</button>
        <button className="btn" onClick={() => setEdit({ kind: "prometheus" })}><Plus size={13} />Add data source</button></div>
      <div className="muted small">Metrics Server is used automatically. Prometheus-compatible backends (Prometheus, Mimir, VictoriaMetrics, Thanos), Loki, Tempo, Alertmanager and Grafana are reached through the Kubernetes API service proxy or a direct URL.</div>
      <table className="tbl"><thead><tr><th>Kind</th><th>Target</th><th></th></tr></thead>
        <tbody>{kinds.map((k) => { const e = eps.data?.[k]; return <tr key={k}><td>{k}</td><td>{e ? (e.url || `svc ${e.namespace}/${e.service}:${e.port}${e.path || ""}`) : <span className="muted">not configured</span>}</td><td><button className="btn sm" onClick={() => setEdit(e || { kind: k })}>Configure</button></td></tr>; })}</tbody></table>
      {edit && <Modal title={`Data source: ${edit.kind}`} onClose={() => setEdit(null)} footer={<button className="btn primary" onClick={async () => { await act(() => api.put(`/clusters/${s.clusterId}/observability`, edit), "Saved"); setEdit(null); eps.reload(); }}>Save</button>}>
        <div className="grid g2">
          <Field label="Kind"><select value={edit.kind} onChange={(e) => setEdit({ ...edit, kind: e.target.value })}>{kinds.map((k) => <option key={k}>{k}</option>)}</select></Field>
          <Field label="Direct URL (optional)"><input value={edit.url || ""} onChange={(e) => setEdit({ ...edit, url: e.target.value })} placeholder="https://prometheus.example.com" /></Field>
          <Field label="Namespace"><input value={edit.namespace || ""} onChange={(e) => setEdit({ ...edit, namespace: e.target.value })} /></Field>
          <Field label="Service"><input value={edit.service || ""} onChange={(e) => setEdit({ ...edit, service: e.target.value })} /></Field>
          <Field label="Port"><input value={edit.port || ""} onChange={(e) => setEdit({ ...edit, port: e.target.value })} /></Field>
          <Field label="Path prefix"><input value={edit.path || ""} onChange={(e) => setEdit({ ...edit, path: e.target.value })} placeholder="/prometheus" /></Field>
          <Field label="Bearer token"><input type="password" value={edit.token || ""} onChange={(e) => setEdit({ ...edit, token: e.target.value })} /></Field>
          <Field label="Tenant (X-Scope-OrgID)"><input value={edit.orgId || ""} onChange={(e) => setEdit({ ...edit, orgId: e.target.value })} /></Field>
        </div>
      </Modal>}
    </div>
  );
}
