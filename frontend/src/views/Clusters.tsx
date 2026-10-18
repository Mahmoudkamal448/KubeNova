import { useMemo, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronRight, ChevronsDownUp, ChevronsUpDown, Download, FolderSync, RefreshCw, Star, Upload, Activity } from "lucide-react";
import { KubeconfigSources } from "./KubeconfigSources";
import { api, downloadUrl } from "../api";
import { useStore } from "../store";
import type { Cluster } from "../types";
import { act, Badge, Bar, Empty, Modal, Field } from "../components/ui";
import { age, bytes, pct } from "../util";

export function ClustersView() {
  const s = useStore();
  const [q, setQ] = useState("");
  const [groupFilter, setGroupFilter] = useState("");
  const [edit, setEdit] = useState<Cluster | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  // Which groups are open, remembered across restarts. With several groups
  // they start collapsed; searching or filtering shows everything.
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    try { return JSON.parse(localStorage.getItem("kubenova.openGroups") || "{}"); } catch { return {}; }
  });
  const saveOpen = (next: Record<string, boolean>) => {
    setOpenGroups(next);
    try { localStorage.setItem("kubenova.openGroups", JSON.stringify(next)); } catch { /* ignore */ }
  };
  const list = useMemo(() => {
    const lq = q.toLowerCase();
    return s.clusters.filter((c) => (!groupFilter || (groupFilter === "★" ? c.favorite : c.group === groupFilter)) &&
      (!lq || `${c.name} ${c.server} ${c.group} ${(c.tags || []).join(" ")} ${c.health?.distribution || ""}`.toLowerCase().includes(lq)));
  }, [s.clusters, q, groupFilter]);
  // Clusters you've connected to are shown together at the top.
  const connected = useMemo(() => list.filter((c) => c.connected), [list]);
  const groups = useMemo(() => {
    const m = new Map<string, Cluster[]>();
    for (const c of list) {
      if (c.connected) continue;
      const g = c.group || "Ungrouped";
      m.set(g, [...(m.get(g) || []), c]);
    }
    return [...m.entries()];
  }, [list]);
  const isOpen = (g: string) => !!q || !!groupFilter || (openGroups[g] ?? groups.length <= 1);
  const setAll = (open: boolean) => saveOpen(Object.fromEntries(groups.map(([g]) => [g, open])));
  const allOpen = groups.every(([g]) => isOpen(g));
  const stats = useMemo(() => ({
    total: s.clusters.length,
    connected: s.clusters.filter((c) => c.connected).length,
    down: s.clusters.filter((c) => c.connected && c.health?.status === "unreachable").length,
    certs: s.clusters.filter((c) => c.health?.certWarning).length,
  }), [s.clusters]);

  return (
    <div className="page">
      <h1>Clusters <Badge>{stats.total}</Badge></h1>
      <div className="grid g4" style={{ marginBottom: 12 }}>
        <div className="card"><h3>Total clusters</h3><div className="stat">{stats.total}</div><div className="muted small">{s.groups.length} groups</div></div>
        <div className="card"><h3>Connected</h3><div className="stat" style={{ color: "var(--ok)" }}>{stats.connected}</div><div className="muted small">KubeNova only talks to clusters you open</div></div>
        <div className="card"><h3>Unreachable</h3><div className="stat" style={{ color: stats.down ? "var(--err)" : undefined }}>{stats.down}</div><div className="muted small">of the connected clusters</div></div>
        <div className="card"><h3><AlertTriangle size={13} />Certificate warnings</h3><div className="stat" style={{ color: stats.certs ? "var(--warn)" : undefined }}>{stats.certs}</div></div>
      </div>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        <input placeholder="Search clusters (name, server, group, tag, distribution)…" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 380 }} />
        <select value={groupFilter} onChange={(e) => setGroupFilter(e.target.value)}>
          <option value="">All groups</option>
          <option value="★">★ Favorites</option>
          {s.groups.map((g) => <option key={g} value={g}>{g}</option>)}
        </select>
        <div className="grow" />
        {groups.length > 1 && !q && !groupFilter && <button className="btn" onClick={() => setAll(!allOpen)}>{allOpen ? <><ChevronsDownUp size={14} />Collapse all</> : <><ChevronsUpDown size={14} />Expand all</>}</button>}
        <button className="btn" onClick={() => setSourcesOpen(true)}><FolderSync size={14} />Kubeconfig sources</button>
        <button className="btn" onClick={() => setImportOpen(true)}><Upload size={14} />Paste kubeconfig</button>
        <a className="btn" href={downloadUrl("/kubeconfigs/merged")} download="kubeconfig-merged.yaml"><Download size={14} />Export merged</a>
        <button className="btn" disabled={busy} title="Contacts every cluster once to refresh its status" onClick={async () => {
          if (s.clusters.length > 10 && !confirm(`Contact all ${s.clusters.length} clusters now to refresh their status? Clusters that need a login (OIDC, cloud SSO…) may ask you to sign in.`)) return;
          setBusy(true); await act(() => api.post("/clusters/health")); await s.loadClusters(); setBusy(false);
        }}>
          <Activity size={14} />{busy ? "Checking…" : "Check all now"}
        </button>
        <button className="btn" onClick={async () => { await api.post("/clusters/reload"); s.loadClusters(); }}><RefreshCw size={14} /></button>
      </div>
      {!s.clusters.length && <Empty>No clusters found. Sync a folder of kubeconfigs (<a href="#" onClick={(e) => { e.preventDefault(); setSourcesOpen(true); }}>Kubeconfig sources</a>), paste one, or import from Rancher / a cloud provider.</Empty>}
      {connected.length > 0 && (
        <div>
          <h2><span className="dot healthy" style={{ marginRight: 6 }} />Connected <span className="muted">({connected.length})</span></h2>
          <div className="grid g3">
            {connected.map((c) => <ClusterCard key={c.id} c={c} showGroup onEdit={() => setEdit(c)} />)}
          </div>
        </div>
      )}
      {groups.map(([g, cs]) => (
        <div key={g}>
          <h2 className="group-h" role="button" tabIndex={0} aria-expanded={isOpen(g)}
            onClick={() => saveOpen({ ...openGroups, [g]: !isOpen(g) })}
            onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), saveOpen({ ...openGroups, [g]: !isOpen(g) }))}>
            {isOpen(g) ? <ChevronDown size={15} /> : <ChevronRight size={15} />}{g} <span className="muted">({cs.length})</span>
            {!isOpen(g) && cs.some((c) => c.favorite) && <Star size={12} fill="var(--warn)" color="var(--warn)" />}
          </h2>
          {isOpen(g) && <div className="grid g3">
            {cs.map((c) => <ClusterCard key={c.id} c={c} onEdit={() => setEdit(c)} />)}
          </div>}
        </div>
      ))}
      {edit && <EditCluster c={edit} onClose={() => setEdit(null)} />}
      {importOpen && <ImportKubeconfig onClose={() => setImportOpen(false)} />}
      {sourcesOpen && <KubeconfigSources onClose={() => setSourcesOpen(false)} />}
    </div>
  );
}

function ClusterCard({ c, onEdit, showGroup }: { c: Cluster; onEdit: () => void; showGroup?: boolean }) {
  const s = useStore();
  const h = c.health;
  const active = s.clusterId === c.id;
  return (
    <div className="card tile" style={{ borderColor: active ? "var(--accent)" : c.color || undefined }} onClick={() => { s.selectCluster(c.id); s.setView("explorer"); }}>
      <div className="row">
        <span className={`dot ${c.connected ? h?.status || "" : ""}`} title={c.connected ? h?.status : "Not connected"} />
        <b className="grow" style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{c.name}</b>
        <button className="btn ghost sm" title="Favorite" onClick={async (e) => { e.stopPropagation(); await api.patch(`/clusters/${c.id}`, { alias: c.name === c.context ? "" : c.name, group: c.group, favorite: !c.favorite, color: c.color, tags: c.tags }); s.loadClusters(); }}>
          <Star size={14} fill={c.favorite ? "var(--warn)" : "none"} color={c.favorite ? "var(--warn)" : "currentColor"} />
        </button>
        {c.connected && <button className="btn ghost sm" title="Stop talking to this cluster until you open it again" onClick={async (e) => { e.stopPropagation(); await api.post(`/clusters/${c.id}/disconnect`); s.loadClusters(); }}>Disconnect</button>}
        <button className="btn ghost sm" onClick={(e) => { e.stopPropagation(); onEdit(); }}>Edit</button>
      </div>
      <div className="muted small" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{c.server}</div>
      <div className="row wrap" style={{ marginTop: 6 }}>
        {h?.version && <Badge kind="info">{h.version}</Badge>}
        {h?.distribution && <Badge>{h.distribution}</Badge>}
        {showGroup && c.group && <Badge>{c.group}</Badge>}
        <Badge>{c.authType}</Badge>
        {c.connected ? <Badge kind="ok">connected</Badge> : <Badge>not connected</Badge>}
        {c.connected && h && h.status !== "unknown" && <Badge kind={h.latencyMs > 500 ? "warn" : ""}>{h.latencyMs} ms</Badge>}
        {(c.tags || []).map((t) => <span key={t} className="chip">{t}</span>)}
      </div>
      {h?.nodes ? (
        <div className="small" style={{ marginTop: 8 }}>
          <div className="row"><span className="grow muted">Nodes ready</span>{h.nodesReady}/{h.nodes}</div>
          <Bar value={pct(h.nodesReady, h.nodes)} kind={h.nodesReady < h.nodes ? "warn" : "ok"} />
          <div className="row muted" style={{ marginTop: 4 }}>
            <span>{(h.cpuCapacityMilli / 1000).toFixed(0)} vCPU</span>·<span>{bytes(h.memoryCapacityBytes)}</span>
            {h.pods > 0 && <>·<span>{h.podsRunning}/{h.pods} pods</span></>}
          </div>
        </div>
      ) : null}
      {c.connected && h?.error && <div className="small" style={{ color: "var(--err)", marginTop: 6, maxHeight: 36, overflow: "hidden" }}>{h.error}</div>}
      {h?.certWarning && <div className="small row" style={{ color: "var(--warn)", marginTop: 6 }}><AlertTriangle size={12} />{h.certWarning}</div>}
      <div className="muted small" style={{ marginTop: 6 }}>{c.lastOpened && !c.lastOpened.startsWith("0001") ? `opened ${age(c.lastOpened)} ago` : "never opened"}{h?.checkedAt ? ` · last checked ${age(h.checkedAt)} ago` : ""}{!c.connected && " · click to connect"}</div>
    </div>
  );
}

function EditCluster({ c, onClose }: { c: Cluster; onClose: () => void }) {
  const s = useStore();
  const [alias, setAlias] = useState(c.name === c.context ? "" : c.name);
  const [group, setGroup] = useState(c.group);
  const [color, setColor] = useState(c.color || "");
  const [tags, setTags] = useState((c.tags || []).join(", "));
  const save = async () => {
    await act(() => api.patch(`/clusters/${c.id}`, { alias, group, favorite: c.favorite, color, tags: tags.split(",").map((t) => t.trim()).filter(Boolean) }), "Saved");
    s.loadClusters();
    onClose();
  };
  return (
    <Modal title={`Edit ${c.context}`} onClose={onClose} footer={<>
      <button className="btn" onClick={async () => { await act(() => api.post(`/clusters/${c.id}/switch`), "kubectl current-context switched"); }}>Set as kubectl current-context</button>
      <button className="btn danger" onClick={async () => { if (confirm(`Remove kubeconfig source ${c.source}?`)) { await act(() => api.del(`/kubeconfigs?path=${encodeURIComponent(c.source)}`)); s.loadClusters(); onClose(); } }}>Remove source</button>
      <button className="btn primary" onClick={save}>Save</button></>}>
      <div className="grid g2">
        <Field label="Display name"><input value={alias} placeholder={c.context} onChange={(e) => setAlias(e.target.value)} /></Field>
        <Field label="Group"><input value={group} list="groups" onChange={(e) => setGroup(e.target.value)} /><datalist id="groups">{s.groups.map((g) => <option key={g} value={g} />)}</datalist></Field>
        <Field label="Color"><input type="color" value={color || "#4f8cff"} onChange={(e) => setColor(e.target.value)} /></Field>
        <Field label="Tags (comma separated)"><input value={tags} onChange={(e) => setTags(e.target.value)} /></Field>
      </div>
      <div className="muted small" style={{ marginTop: 10 }}>Context: {c.context} · Source: {c.source} · User: {c.user} · Auth: {c.authType}</div>
    </Modal>
  );
}

function ImportKubeconfig({ onClose }: { onClose: () => void }) {
  const s = useStore();
  const [name, setName] = useState("pasted");
  const [content, setContent] = useState("");
  return (
    <Modal title="Paste kubeconfig" onClose={onClose} footer={<button className="btn primary" onClick={async () => { if (await act(() => api.post("/kubeconfigs/import", { name, content }), "Imported")) { s.loadClusters(); onClose(); } }}>Import</button>}>
      <div className="col">
        <Field label="Name"><input value={name} onChange={(e) => setName(e.target.value)} /></Field>
        <textarea rows={18} value={content} onChange={(e) => setContent(e.target.value)} placeholder="apiVersion: v1&#10;kind: Config&#10;…" />
      </div>
    </Modal>
  );
}
