import { useMemo, useState } from "react";
import { RefreshCw } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { Empty, ErrorBox, useAsync } from "../components/ui";

const layers = ["Ingress", "HTTPRoute", "Service", "Deployment", "StatefulSet", "DaemonSet", "CronJob", "Job", "ReplicaSet", "Pod", "ConfigMap", "Secret", "PersistentVolumeClaim", "ServiceAccount"];
const col: Record<string, number> = { Ingress: 0, HTTPRoute: 0, Service: 1, Deployment: 2, StatefulSet: 2, DaemonSet: 2, CronJob: 2, Job: 3, ReplicaSet: 3, Pod: 4, ConfigMap: 5, Secret: 5, PersistentVolumeClaim: 5, ServiceAccount: 5 };
const color = (h: string) => (h === "ok" ? "var(--ok)" : h === "error" ? "var(--err)" : h === "warn" ? "var(--warn)" : "var(--border2)");

/** Live dependency graph: Ingress → Service → Workload → Pod → Config/Storage. */
export function TopologyView() {
  const s = useStore();
  const [ns, setNs] = useState(s.namespace || "default");
  const [hideRS, setHideRS] = useState(true);
  const [focus, setFocus] = useState<string | null>(null);
  const g = useAsync(() => (s.clusterId ? api.get(`/clusters/${s.clusterId}/topology?namespace=${ns}`) : Promise.resolve(null)), [s.clusterId, ns]);
  const layout = useMemo(() => {
    if (!g.data) return null;
    let nodes = (g.data.nodes || []) as any[];
    let edges = (g.data.edges || []) as any[];
    if (hideRS) {
      // Collapse ReplicaSets: Deployment → Pod directly.
      const rs = new Set(nodes.filter((n) => n.kind === "ReplicaSet").map((n) => n.id));
      const parent = new Map(edges.filter((e) => rs.has(e.to)).map((e) => [e.to, e.from]));
      edges = edges.flatMap((e) => (rs.has(e.to) ? [] : rs.has(e.from) ? (parent.get(e.from) ? [{ ...e, from: parent.get(e.from) }] : []) : [e]));
      nodes = nodes.filter((n) => !rs.has(n.id));
    }
    if (focus) {
      const keep = new Set([focus]);
      let changed = true;
      while (changed) {
        changed = false;
        for (const e of edges) {
          if (keep.has(e.from) && !keep.has(e.to)) { keep.add(e.to); changed = true; }
          if (keep.has(e.to) && !keep.has(e.from) && e.type !== "mounts" && e.type !== "uses") { keep.add(e.from); changed = true; }
        }
      }
      nodes = nodes.filter((n) => keep.has(n.id));
      edges = edges.filter((e) => keep.has(e.from) && keep.has(e.to));
    }
    const perCol: Record<number, number> = {};
    const pos = new Map<string, { x: number; y: number }>();
    nodes.sort((a, b) => layers.indexOf(a.kind) - layers.indexOf(b.kind) || a.name.localeCompare(b.name));
    for (const n of nodes) {
      const c = col[n.kind] ?? 5;
      const i = (perCol[c] = (perCol[c] ?? -1) + 1);
      pos.set(n.id, { x: 20 + c * 250, y: 20 + i * 54 });
    }
    const h = Math.max(...Object.values(perCol).map((v) => v + 1), 1) * 54 + 40;
    return { nodes, edges, pos, h };
  }, [g.data, hideRS, focus]);
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster.</Empty></div>;
  return (
    <div className="page">
      <h1>Topology graph</h1>
      <div className="row" style={{ marginBottom: 10 }}>
        <select value={ns} onChange={(e) => { setNs(e.target.value); setFocus(null); }}>{s.namespaces.map((n) => <option key={n}>{n}</option>)}</select>
        <label className="inline"><input type="checkbox" checked={hideRS} onChange={(e) => setHideRS(e.target.checked)} />Collapse ReplicaSets</label>
        {focus && <button className="btn" onClick={() => setFocus(null)}>Clear focus ({focus.split("/").pop()})</button>}
        <button className="btn" onClick={g.reload}><RefreshCw size={13} /></button>
        <span className="muted small">Click a node to focus on its dependency chain.</span>
      </div>
      <ErrorBox error={g.error} />
      {layout && (layout.nodes.length ? (
        <svg width={6 * 250 + 40} height={layout.h}>
          <defs><marker id="arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="var(--fg3)" /></marker></defs>
          {layout.edges.map((e: any, i: number) => {
            const a = layout.pos.get(e.from), b = layout.pos.get(e.to);
            if (!a || !b) return null;
            const x1 = a.x + 210, y1 = a.y + 20, x2 = b.x, y2 = b.y + 20;
            return <path key={i} d={`M${x1},${y1} C${x1 + 30},${y1} ${x2 - 30},${y2} ${x2},${y2}`} fill="none" stroke={e.type === "mounts" || e.type === "uses" ? "var(--accent2)" : "var(--fg3)"} strokeDasharray={e.type === "mounts" || e.type === "uses" ? "4 3" : undefined} markerEnd="url(#arr)" opacity={0.7} />;
          })}
          {layout.nodes.map((n: any) => {
            const p = layout.pos.get(n.id)!;
            return <g key={n.id} onClick={() => setFocus(n.id)} style={{ cursor: "pointer" }}>
              <rect x={p.x} y={p.y} width={210} height={40} rx={8} fill="var(--bg2)" stroke={color(n.health)} strokeWidth={focus === n.id ? 2.5 : 1.3} />
              <text x={p.x + 10} y={p.y + 15} fontSize={10.5} fill="var(--fg3)">{n.kind}{n.status ? ` · ${n.status}` : ""}</text>
              <text x={p.x + 10} y={p.y + 31} fontSize={12} fill="var(--fg)">{n.name.length > 28 ? n.name.slice(0, 27) + "…" : n.name}</text>
            </g>;
          })}
        </svg>
      ) : <Empty>No resources in {ns}.</Empty>)}
    </div>
  );
}
