import { useState } from "react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, Modal } from "../components/ui";
import { UnifiedDiff } from "../components/DiffView";

export function CompareView() {
  const s = useStore();
  const [a, setA] = useState(s.clusterId);
  const [b, setB] = useState(s.clusters.find((c) => c.id !== s.clusterId)?.id || "");
  const [resource, setResource] = useState("deployments");
  const [ns, setNs] = useState(s.namespace || "default");
  const [res, setRes] = useState<any[] | null>(null);
  const [diff, setDiff] = useState<any>(null);
  const counts = (res || []).reduce((m: any, e: any) => ({ ...m, [e.state]: (m[e.state] || 0) + 1 }), {});
  return (
    <div className="page">
      <h1>Cluster comparison</h1>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        <select value={a} onChange={(e) => setA(e.target.value)}>{s.clusters.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        <span>vs</span>
        <select value={b} onChange={(e) => setB(e.target.value)}>{s.clusters.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        <input value={resource} onChange={(e) => setResource(e.target.value)} placeholder="resource (deployments, configmaps, crds…)" />
        <input value={ns} onChange={(e) => setNs(e.target.value)} placeholder="namespace (empty = all)" />
        <button className="btn primary" onClick={async () => setRes((await act(() => api.post("/compare", { a, b, resource, namespace: ns }))) || null)}>Compare</button>
      </div>
      {res && <div className="row" style={{ marginBottom: 8 }}>{Object.entries(counts).map(([k, v]) => <Badge key={k} kind={k === "same" ? "ok" : k === "different" ? "warn" : "info"}>{k}: {String(v)}</Badge>)}</div>}
      {res && <table className="tbl"><thead><tr><th>Name</th><th>Namespace</th><th>State</th><th></th></tr></thead>
        <tbody>{res.map((e) => <tr key={e.namespace + e.name}><td>{e.name}</td><td>{e.namespace}</td><td><Badge kind={e.state === "same" ? "ok" : e.state === "different" ? "warn" : "info"}>{e.state === "onlyA" ? "only in A" : e.state === "onlyB" ? "only in B" : e.state}</Badge></td>
          <td>{e.diff && <button className="btn sm" onClick={() => setDiff(e)}>View diff</button>}</td></tr>)}</tbody></table>}
      {res && !res.length && <Empty>Nothing to compare.</Empty>}
      {diff && <Modal title={`${diff.kind} ${diff.name}`} onClose={() => setDiff(null)}><UnifiedDiff text={diff.diff} /></Modal>}
    </div>
  );
}
