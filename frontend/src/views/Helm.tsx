import { useState } from "react";
import { History, Package, Plus, RefreshCw, Search, Sparkles, Trash2, Undo2 } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Confirm, Empty, ErrorBox, Field, Modal, Status, Tabs, useAsync } from "../components/ui";
import { YamlEditor } from "../components/YamlEditor";
import { UnifiedDiff } from "../components/DiffView";

type Tab = "releases" | "charts" | "repos";

export function HelmView() {
  const s = useStore();
  const [tab, setTab] = useState<Tab>("releases");
  return (
    <div className="page" style={{ padding: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "14px 20px 0" }}><h1><Package size={18} />Helm</h1></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "releases", label: "Releases" }, { id: "charts", label: "Charts" }, { id: "repos", label: "Repositories & OCI" }]} />
      <div className="grow" style={{ padding: 16, overflow: "auto" }}>
        {tab === "releases" && (s.clusterId ? <Releases /> : <Empty>Select a cluster.</Empty>)}
        {tab === "charts" && <Charts />}
        {tab === "repos" && <Repos />}
      </div>
    </div>
  );
}

function Releases() {
  const s = useStore();
  const rel = useAsync(() => api.get(`/clusters/${s.clusterId}/helm/releases?namespace=${s.namespace}`), [s.clusterId, s.namespace]);
  const [sel, setSel] = useState<any>(null);
  return (
    <>
      <div className="row"><button className="btn" onClick={rel.reload}><RefreshCw size={13} /></button></div>
      <ErrorBox error={rel.error} />
      <table className="tbl" style={{ marginTop: 8 }}><thead><tr><th>Name</th><th>Namespace</th><th>Chart</th><th>App version</th><th>Revision</th><th>Status</th><th>Updated</th></tr></thead>
        <tbody>{(rel.data || []).map((r: any) => <tr key={r.namespace + r.name} onClick={() => setSel(r)} style={{ cursor: "pointer" }}><td><b>{r.name}</b></td><td>{r.namespace}</td><td>{r.chart}</td><td>{r.app_version}</td><td>{r.revision}</td><td><Status s={r.status} /></td><td className="muted">{r.updated?.slice(0, 19)}</td></tr>)}</tbody></table>
      {rel.data && !rel.data.length && <Empty>No releases.</Empty>}
      {sel && <ReleaseDrawer r={sel} onClose={() => { setSel(null); rel.reload(); }} />}
    </>
  );
}

function ReleaseDrawer({ r, onClose }: { r: any; onClose: () => void }) {
  const s = useStore();
  const base = `/clusters/${s.clusterId}/helm/releases/${r.namespace}/${r.name}`;
  const [tab, setTab] = useState<"values" | "manifest" | "notes" | "history" | "upgrade">("values");
  const content = useAsync(() => (["values", "manifest", "notes"].includes(tab) ? api.get(`${base}/${tab}`) : Promise.resolve(null)), [tab, base]);
  const hist = useAsync(() => api.get(`${base}/history`), [base]);
  const [del, setDel] = useState(false);
  return (
    <div className="drawer">
      <div className="drawer-head"><b className="grow">{r.name} <span className="muted small">{r.chart}</span></b>
        <button className="btn ai sm" onClick={async () => s.openAI({ task: "explain-helm", context: `Release ${r.name} chart ${r.chart}\nValues:\n${(await api.get(`${base}/values`)).content}` })}><Sparkles size={13} />Explain</button>
        <button className="btn sm danger" onClick={() => setDel(true)}><Trash2 size={13} />Uninstall</button>
        <button className="btn ghost sm" onClick={onClose}>✕</button></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "values", label: "Values" }, { id: "manifest", label: "Manifest" }, { id: "notes", label: "Notes" }, { id: "history", label: "History" }, { id: "upgrade", label: "Upgrade / Values editor" }]} />
      <div className="grow" style={{ overflow: "auto", padding: 12 }}>
        {content.data && <YamlEditor value={content.data.content} readOnly height="calc(100vh - 200px)" />}
        {tab === "history" && <table className="tbl small"><thead><tr><th>Rev</th><th>Status</th><th>Chart</th><th>App</th><th>Updated</th><th>Description</th><th></th></tr></thead>
          <tbody>{[...(hist.data || [])].reverse().map((h: any) => <tr key={h.revision}><td>{h.revision}</td><td><Status s={h.status} /></td><td>{h.chart}</td><td>{h.app_version}</td><td>{h.updated?.slice(0, 19)}</td><td style={{ whiteSpace: "normal" }}>{h.description}</td>
            <td>{h.status !== "deployed" && <button className="btn sm" onClick={() => act(() => api.post(`${base}/rollback`, { revision: h.revision }), `Rolled back to ${h.revision}`).then(hist.reload)}><Undo2 size={12} />Rollback</button>}</td></tr>)}</tbody></table>}
        {tab === "upgrade" && <InstallForm initial={{ name: r.name, namespace: r.namespace, chart: "", version: "" }} upgradeOf={base} />}
      </div>
      {del && <Confirm text={`Uninstall release ${r.name}?`} onClose={() => setDel(false)} onYes={async () => { await act(() => api.del(base), "Uninstalled"); onClose(); }} />}
    </div>
  );
}

function InstallForm({ initial, upgradeOf, onDone }: { initial: { name: string; namespace: string; chart: string; version: string }; upgradeOf?: string; onDone?: () => void }) {
  const s = useStore();
  const [f, setF] = useState({ ...initial, values: "", createNamespace: true, wait: false, atomic: false });
  const [out, setOut] = useState("");
  const [diff, setDiff] = useState<string | null>(null);
  const loadDefaults = async () => {
    if (upgradeOf) setF({ ...f, values: (await api.get(`${upgradeOf}/values`)).content });
    else if (f.chart) setF({ ...f, values: (await act(() => api.get(`/helm/chart?chart=${encodeURIComponent(f.chart)}&version=${f.version}`)))?.content || "" });
  };
  const run = async (dryRun: boolean) => {
    const r = await act(() => api.post(`/clusters/${s.clusterId}/helm/upgrade`, { ...f, dryRun }), dryRun ? "Dry run complete" : "Release installed/upgraded");
    if (r) { setOut(r.output); if (!dryRun) onDone?.(); }
  };
  return (
    <div className="col">
      <div className="grid g4">
        <Field label="Release name"><input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Namespace"><input value={f.namespace} onChange={(e) => setF({ ...f, namespace: e.target.value })} /></Field>
        <Field label="Chart (repo/chart, oci://…, path)"><input value={f.chart} onChange={(e) => setF({ ...f, chart: e.target.value })} placeholder={upgradeOf ? "e.g. bitnami/nginx" : ""} /></Field>
        <Field label="Version"><input value={f.version} onChange={(e) => setF({ ...f, version: e.target.value })} /></Field>
      </div>
      <div className="row wrap">
        <label className="inline"><input type="checkbox" checked={f.createNamespace} onChange={(e) => setF({ ...f, createNamespace: e.target.checked })} />Create namespace</label>
        <label className="inline"><input type="checkbox" checked={f.wait} onChange={(e) => setF({ ...f, wait: e.target.checked })} />Wait</label>
        <label className="inline"><input type="checkbox" checked={f.atomic} onChange={(e) => setF({ ...f, atomic: e.target.checked })} />Atomic</label>
        <div className="grow" />
        <button className="btn" onClick={loadDefaults}>{upgradeOf ? "Load current values" : "Load chart defaults"}</button>
        <button className="btn" onClick={async () => setDiff((await act(() => api.post(`/clusters/${s.clusterId}/helm/diff`, f)))?.diff ?? null)}>Diff</button>
        <button className="btn" onClick={() => run(true)}>Dry run</button>
        <button className="btn primary" onClick={() => run(false)}>{upgradeOf ? "Upgrade" : "Install"}</button>
      </div>
      <div style={{ height: 380 }}><YamlEditor value={f.values} onChange={(v) => setF({ ...f, values: v })} /></div>
      {diff !== null && <UnifiedDiff text={diff} />}
      {out && <pre style={{ maxHeight: 300 }}>{out}</pre>}
    </div>
  );
}

function Charts() {
  const s = useStore();
  const [q, setQ] = useState("");
  const [res, setRes] = useState<any[] | null>(null);
  const [install, setInstall] = useState<any>(null);
  return (
    <div className="col">
      <div className="row"><input className="grow" placeholder="Search charts in configured repositories…" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={async (e) => e.key === "Enter" && setRes((await act(() => api.get(`/helm/search?q=${encodeURIComponent(q)}`))) || [])} />
        <button className="btn primary" onClick={async () => setRes((await act(() => api.get(`/helm/search?q=${encodeURIComponent(q)}`))) || [])}><Search size={13} />Search</button>
        <button className="btn" onClick={() => setInstall({ name: "", namespace: s.namespace || "default", chart: "oci://", version: "" })}><Plus size={13} />Install from OCI / path</button></div>
      <table className="tbl"><thead><tr><th>Chart</th><th>Version</th><th>App version</th><th>Description</th><th></th></tr></thead>
        <tbody>{(res || []).map((c) => <tr key={c.name + c.version}><td><b>{c.name}</b></td><td>{c.version}</td><td>{c.app_version}</td><td style={{ whiteSpace: "normal" }}>{c.description}</td>
          <td><button className="btn sm primary" disabled={!s.clusterId} onClick={() => setInstall({ name: c.name.split("/").pop(), namespace: s.namespace || "default", chart: c.name, version: c.version })}>Install</button></td></tr>)}</tbody></table>
      {res && !res.length && <Empty>No charts found. Add repositories first.</Empty>}
      {install && <Modal wide title={`Install ${install.chart}`} onClose={() => setInstall(null)}><InstallForm initial={install} onDone={() => setInstall(null)} /></Modal>}
    </div>
  );
}

function Repos() {
  const repos = useAsync(() => api.get("/helm/repos"), []);
  const [f, setF] = useState({ name: "", url: "", username: "", password: "" });
  const [oci, setOci] = useState({ host: "", username: "", password: "" });
  return (
    <div className="col">
      <div className="card"><h3>Add repository</h3>
        <div className="row wrap"><input placeholder="name" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /><input className="grow" placeholder="https://charts.example.com" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} />
          <input placeholder="username (optional)" value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} /><input type="password" placeholder="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} />
          <button className="btn primary" onClick={async () => { await act(() => api.post("/helm/repos", f), "Repository added"); repos.reload(); }}>Add</button></div>
      </div>
      <div className="card"><h3>OCI registry login</h3>
        <div className="row wrap"><input className="grow" placeholder="registry host, e.g. ghcr.io" value={oci.host} onChange={(e) => setOci({ ...oci, host: e.target.value })} /><input placeholder="username" value={oci.username} onChange={(e) => setOci({ ...oci, username: e.target.value })} /><input type="password" placeholder="password / token" value={oci.password} onChange={(e) => setOci({ ...oci, password: e.target.value })} />
          <button className="btn primary" onClick={() => act(() => api.post("/helm/registry/login", oci), "Logged in")}>Login</button></div>
      </div>
      <div className="row"><b className="grow">Repositories</b><button className="btn" onClick={() => act(() => api.post("/helm/repos/update"), "Repositories updated")}><History size={13} />Update all</button></div>
      <table className="tbl"><thead><tr><th>Name</th><th>URL</th><th></th></tr></thead>
        <tbody>{(repos.data || []).map((r: any) => <tr key={r.name}><td>{r.name}</td><td>{r.url}</td><td><button className="btn sm danger" onClick={async () => { await act(() => api.del(`/helm/repos/${r.name}`)); repos.reload(); }}><Trash2 size={12} /></button></td></tr>)}</tbody></table>
      <Badge>Helm CLI is used under the hood; bundle it in the tools directory for air-gapped installs.</Badge>
    </div>
  );
}
