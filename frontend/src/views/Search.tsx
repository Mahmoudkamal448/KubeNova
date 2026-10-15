import { useState } from "react";
import { Search as SearchIcon } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, Status } from "../components/ui";
import { age } from "../util";

export function SearchView() {
  const s = useStore();
  const [q, setQ] = useState("");
  const [kinds, setKinds] = useState("");
  const [scope, setScope] = useState<string[]>([]);
  const [hits, setHits] = useState<any[] | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setHits((await act(() => api.post("/search", { query: q, clusters: scope, kinds: kinds.split(",").map((k) => k.trim()).filter(Boolean) }))) || []);
    setBusy(false);
  };
  return (
    <div className="page">
      <h1>Multi-cluster search</h1>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        <input autoFocus className="grow" placeholder="Name or label (app=web) across all reachable clusters…" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === "Enter" && run()} />
        <input placeholder="kinds: pods,deployments,… (default: common)" value={kinds} onChange={(e) => setKinds(e.target.value)} style={{ width: 280 }} />
        <select multiple value={scope} onChange={(e) => setScope([...e.target.selectedOptions].map((o) => o.value))} style={{ height: 30, width: 200 }} title="Limit to clusters (empty = all healthy)">
          {s.clusters.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <button className="btn primary" disabled={busy || !q} onClick={run}><SearchIcon size={13} />{busy ? "Searching…" : "Search"}</button>
      </div>
      {hits && <Badge>{hits.length} results</Badge>}
      {hits && <table className="tbl" style={{ marginTop: 8 }}><thead><tr><th>Name</th><th>Kind</th><th>Namespace</th><th>Cluster</th><th>Status</th><th>Age</th></tr></thead>
        <tbody>{hits.map((h, i) => <tr key={i} style={{ cursor: "pointer" }} onClick={async () => {
          await s.selectCluster(h.clusterId);
          const r = useStore.getState().resources.find((x) => x.resource === h.resource && x.group === h.group);
          if (r) { s.setNamespace(h.row.namespace || ""); s.selectResource(r); }
        }}><td><b>{h.row.name}</b></td><td>{h.row.kind}</td><td>{h.row.namespace}</td><td>{h.clusterName}</td><td><Status s={h.row.status} /></td><td>{age(h.row.created)}</td></tr>)}</tbody></table>}
      {hits && !hits.length && <Empty>No matches.</Empty>}
    </div>
  );
}
