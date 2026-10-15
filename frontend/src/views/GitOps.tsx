import { useMemo, useState } from "react";
import { GitBranch, Pause, Play, RefreshCw, RotateCw, Sparkles, Undo2 } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Status, Tabs, useAsync } from "../components/ui";
import { age } from "../util";

export function GitOpsView() {
  const s = useStore();
  const apps = useAsync(() => (s.clusterId ? api.get(`/clusters/${s.clusterId}/gitops/apps`) : Promise.resolve([])), [s.clusterId]);
  const [engine, setEngine] = useState<"all" | "flux" | "argocd" | "graph">("all");
  const [sel, setSel] = useState<any>(null);
  const [q, setQ] = useState("");
  const list = useMemo(() => (apps.data || []).filter((a: any) => (engine === "all" || engine === "graph" || a.engine === engine) && (!q || `${a.name} ${a.namespace} ${a.kind} ${a.source}`.toLowerCase().includes(q.toLowerCase()))), [apps.data, engine, q]);
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster.</Empty></div>;
  const counts = (apps.data || []).reduce((m: any, a: any) => ({ ...m, [a.health]: (m[a.health] || 0) + 1 }), {});
  const action = (a: any, action: string, arg = "") => act(() => api.post(`/clusters/${s.clusterId}/gitops/action`, { app: a, action, arg }), `${action} requested`).then(() => setTimeout(apps.reload, 1500));
  return (
    <div className="page">
      <h1><GitBranch size={18} />GitOps <span className="muted small">FluxCD & Argo CD</span></h1>
      <div className="row wrap" style={{ marginBottom: 10 }}>
        {Object.entries(counts).map(([k, v]) => <Badge key={k} kind={k === "Healthy" ? "ok" : k === "Degraded" ? "error" : "warn"}>{k}: {String(v)}</Badge>)}
        <div className="grow" />
        <input placeholder="Filter…" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn" onClick={apps.reload}><RefreshCw size={13} /></button>
      </div>
      <Tabs value={engine} onChange={setEngine} tabs={[{ id: "all", label: "All" }, { id: "flux", label: "Flux" }, { id: "argocd", label: "Argo CD" }, { id: "graph", label: "Flux visualization" }]} />
      <ErrorBox error={apps.error} />
      {engine === "graph" ? <FluxGraph apps={(apps.data || []).filter((a: any) => a.engine === "flux")} /> : (
        <table className="tbl" style={{ marginTop: 8 }}><thead><tr><th>Name</th><th>Kind</th><th>Namespace</th><th>Health</th><th>Sync</th><th>Revision</th><th>Source</th><th>Drift</th><th>Last applied</th><th></th></tr></thead>
          <tbody>{list.map((a: any) => (
            <tr key={a.engine + a.kind + a.namespace + a.name} onClick={() => setSel(a)} style={{ cursor: "pointer" }}>
              <td><b>{a.name}</b></td><td><Badge>{a.engine === "flux" ? "Flux " : "Argo "}{a.kind}</Badge></td><td>{a.namespace}</td><td><Status s={a.health} /></td><td><Status s={a.sync} /></td>
              <td className="mono small">{(a.revision || "").slice(0, 24)}</td><td className="small" style={{ maxWidth: 260 }}>{a.source}{a.path ? ` (${a.path})` : ""}</td>
              <td>{a.drift?.length ? <Badge kind="warn">{a.drift.length}</Badge> : <span className="muted">-</span>}</td><td className="muted">{a.lastApplied ? age(a.lastApplied) : "-"}</td>
              <td onClick={(e) => e.stopPropagation()} className="row">
                {a.engine === "flux" ? <>
                  <button className="btn sm" title="Reconcile" onClick={() => action(a, "reconcile")}><RotateCw size={12} /></button>
                  <button className="btn sm" title={a.suspended ? "Resume" : "Suspend"} onClick={() => action(a, a.suspended ? "resume" : "suspend")}>{a.suspended ? <Play size={12} /> : <Pause size={12} />}</button>
                </> : a.kind === "Application" && <>
                  <button className="btn sm" onClick={() => action(a, "sync")}>Sync</button>
                  <button className="btn sm" title="Hard refresh" onClick={() => action(a, "refresh")}><RotateCw size={12} /></button>
                </>}
              </td>
            </tr>
          ))}</tbody></table>
      )}
      {apps.data && !apps.data.length && <Empty>No Flux or Argo CD objects found in this cluster.</Empty>}
      {sel && (
        <div className="drawer">
          <div className="drawer-head"><b className="grow">{sel.kind} {sel.namespace}/{sel.name}</b>
            {sel.health !== "Healthy" && <button className="btn ai sm" onClick={() => s.openAI({ task: sel.engine === "flux" ? "flux-failure" : "argocd-failure", prompt: `Explain the failure of ${sel.kind} ${sel.namespace}/${sel.name}.`, context: JSON.stringify(sel, null, 1) })}><Sparkles size={13} />Explain failure</button>}
            <button className="btn ghost sm" onClick={() => setSel(null)}>✕</button></div>
          <div style={{ padding: 14, overflow: "auto" }} className="col">
            <div className="row wrap"><Status s={sel.health} /><Status s={sel.sync} />{sel.suspended && <Badge kind="warn">suspended</Badge>}</div>
            <div><b>Source:</b> {sel.source} {sel.path}</div>
            <div><b>Revision:</b> <span className="mono">{sel.revision}</span></div>
            {sel.message && <pre style={{ whiteSpace: "pre-wrap" }}>{sel.message}</pre>}
            {sel.dependsOn?.length > 0 && <div><b>Depends on:</b> {sel.dependsOn.join(", ")}</div>}
            <h2>Drift</h2>
            {sel.drift?.length ? <table className="tbl small"><thead><tr><th>Kind</th><th>Name</th><th>Namespace</th><th>Status</th><th>Health</th><th>Message</th></tr></thead>
              <tbody>{sel.drift.map((d: any, i: number) => <tr key={i}><td>{d.kind}</td><td>{d.name}</td><td>{d.namespace}</td><td><Status s={d.status} /></td><td><Status s={d.health} /></td><td>{d.message}</td></tr>)}</tbody></table> : <div className="muted">No drift detected.</div>}
            <h2>History</h2>
            {sel.history?.length ? <table className="tbl small"><thead><tr><th>Revision</th><th>Deployed</th><th></th></tr></thead>
              <tbody>{sel.history.map((h: any, i: number) => <tr key={i}><td className="mono">{h.revision}</td><td>{h.deployedAt}</td><td>{sel.engine === "argocd" && i > 0 && <button className="btn sm" onClick={() => action(sel, "rollback", String(h.id))}><Undo2 size={12} />Rollback</button>}</td></tr>)}</tbody></table> : <div className="muted">No history recorded.</div>}
            {sel.inventory?.length > 0 && <><h2>Managed resources ({sel.inventory.length})</h2><pre style={{ maxHeight: 260 }}>{sel.inventory.join("\n")}</pre></>}
          </div>
        </div>
      )}
    </div>
  );
}

/** Dependency graph of Flux sources → kustomizations/helmreleases. */
function FluxGraph({ apps }: { apps: any[] }) {
  const sources = apps.filter((a) => a.group.startsWith("source."));
  const consumers = apps.filter((a) => !a.group.startsWith("source.") && !a.group.startsWith("notification") && !a.group.startsWith("image"));
  const pos = new Map<string, { x: number; y: number }>();
  sources.forEach((a, i) => pos.set(`${a.kind}/${a.name}`, { x: 40, y: 30 + i * 56 }));
  consumers.forEach((a, i) => pos.set(`${a.kind}/${a.name}`, { x: 420, y: 30 + i * 56 }));
  const h = Math.max(sources.length, consumers.length) * 56 + 40;
  const color = (hs: string) => (hs === "Healthy" ? "var(--ok)" : hs === "Degraded" ? "var(--err)" : "var(--warn)");
  return (
    <svg width="100%" height={h} viewBox={`0 0 800 ${h}`} style={{ marginTop: 10 }}>
      {consumers.map((c) => {
        const src = (c.source || "").split(" : ")[0];
        const p1 = pos.get(src), p2 = pos.get(`${c.kind}/${c.name}`);
        const deps = (c.dependsOn || []).map((d: string) => pos.get(`Kustomization/${d}`) || pos.get(`HelmRelease/${d}`)).filter(Boolean);
        return <g key={c.name + c.kind}>
          {p1 && p2 && <path d={`M${p1.x + 300},${p1.y + 18} C${p1.x + 360},${p1.y + 18} ${p2.x - 60},${p2.y + 18} ${p2.x},${p2.y + 18}`} fill="none" stroke="var(--border2)" />}
          {deps.map((d: any, i: number) => <path key={i} d={`M${d.x + 300},${d.y + 18} C${d.x + 340},${d.y + 18} ${p2!.x + 340},${p2!.y + 18} ${p2!.x + 300},${p2!.y + 18}`} fill="none" stroke="var(--accent2)" strokeDasharray="4 3" />)}
        </g>;
      })}
      {[...sources, ...consumers].map((a) => {
        const p = pos.get(`${a.kind}/${a.name}`)!;
        return <g key={a.kind + a.name}><rect x={p.x} y={p.y} width={300} height={36} rx={8} fill="var(--bg2)" stroke={color(a.health)} />
          <text x={p.x + 10} y={p.y + 15} fontSize={11} fill="var(--fg3)">{a.kind}</text><text x={p.x + 10} y={p.y + 29} fontSize={12} fill="var(--fg)">{a.namespace}/{a.name}</text></g>;
      })}
    </svg>
  );
}
