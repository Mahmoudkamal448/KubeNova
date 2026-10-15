import { useEffect, useState } from "react";
import { Download, LogIn, Plus, RefreshCw, Server, SquareTerminal, Trash2 } from "lucide-react";
import { api, openExternal } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Field, JsonView, Modal, Status, Tabs, useAsync } from "../components/ui";

export function RancherView() {
  const servers = useAsync(() => api.get("/rancher/servers"), []);
  const [sel, setSel] = useState<string>("");
  const [add, setAdd] = useState(false);
  useEffect(() => { if (!sel && servers.data?.length) setSel(servers.data[0].id); }, [servers.data, sel]);
  const server = (servers.data || []).find((x: any) => x.id === sel);
  return (
    <div className="split" style={{ flex: 1 }}>
      <div className="side">
        <div className="sec">Rancher servers <Plus size={13} style={{ cursor: "pointer" }} onClick={() => setAdd(true)} /></div>
        {(servers.data || []).map((x: any) => <div key={x.id} className={`item ${x.id === sel ? "active" : ""}`} onClick={() => setSel(x.id)}><span>{x.name}</span>{x.token ? <Badge kind="ok">auth</Badge> : <Badge kind="warn">login</Badge>}</div>)}
        {!servers.data?.length && <div className="muted small" style={{ padding: 8 }}>Add a Rancher server to discover clusters.</div>}
      </div>
      <div className="page">{server ? <ServerPanel key={server.id} sv={server} reload={servers.reload} /> : <Empty><Server size={28} /><div>Connect Rancher via API token, username/password (local, AD, LDAP) or browser SSO.</div><button className="btn primary" onClick={() => setAdd(true)}>Add server</button></Empty>}</div>
      {add && <AddServer onClose={() => { setAdd(false); servers.reload(); }} />}
    </div>
  );
}

function AddServer({ onClose }: { onClose: () => void }) {
  const [f, setF] = useState({ name: "", url: "https://", token: "", insecure: false });
  return (
    <Modal title="Add Rancher server" onClose={onClose} footer={<button className="btn primary" onClick={async () => { if (await act(() => api.post("/rancher/servers", f), "Server added")) onClose(); }}>Add</button>}>
      <div className="grid g2">
        <Field label="Name"><input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="URL"><input value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} /></Field>
        <Field label="API token (optional — or log in afterwards)"><input type="password" value={f.token} onChange={(e) => setF({ ...f, token: e.target.value })} placeholder="token-xxxxx:yyyy" /></Field>
        <label className="inline"><input type="checkbox" checked={f.insecure} onChange={(e) => setF({ ...f, insecure: e.target.checked })} />Skip TLS verification (self-signed)</label>
      </div>
    </Modal>
  );
}

function ServerPanel({ sv, reload }: { sv: any; reload: () => void }) {
  const [tab, setTab] = useState<"clusters" | "provisioning" | "fleet" | "register" | "login">(sv.token ? "clusters" : "login");
  return (
    <>
      <h1 className="row"><Server size={18} />{sv.name} <span className="muted small">{sv.url}</span>{sv.version && <Badge kind="info">{sv.version}</Badge>}
        <div className="grow" /><button className="btn" onClick={() => openExternal(sv.url + "/dashboard/c/local/explorer")}>Open Cluster Explorer</button>
        <button className="btn danger" onClick={async () => { if (confirm("Remove server?")) { await api.del(`/rancher/servers/${sv.id}`); reload(); } }}><Trash2 size={13} /></button></h1>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "clusters", label: "Clusters & projects" }, { id: "provisioning", label: "RKE2/K3s & machine pools" }, { id: "fleet", label: "Fleet" }, { id: "register", label: "Register cluster" }, { id: "login", label: "Authentication" }]} />
      <div style={{ paddingTop: 12 }}>
        {tab === "login" && <Login sv={sv} onDone={() => { reload(); setTab("clusters"); }} />}
        {tab === "clusters" && <Clusters sv={sv} />}
        {tab === "provisioning" && <Provisioning sv={sv} />}
        {tab === "fleet" && <Fleet sv={sv} />}
        {tab === "register" && <Register sv={sv} />}
      </div>
    </>
  );
}

function Login({ sv, onDone }: { sv: any; onDone: () => void }) {
  const [f, setF] = useState({ provider: "local", username: "", password: "" });
  const [token, setToken] = useState("");
  const [sso, setSso] = useState("");
  const startSSO = async () => {
    const r = await act(() => api.post(`/rancher/servers/${sv.id}/sso`));
    if (!r) return;
    openExternal(r.url);
    setSso("Waiting for browser login…");
    for (let i = 0; i < 120; i++) {
      await new Promise((res) => setTimeout(res, 2500));
      const p = await api.get(`/rancher/servers/${sv.id}/sso/${r.requestId}`).catch(() => ({ done: false }));
      if (p.done) { setSso("Logged in"); onDone(); return; }
    }
    setSso("Timed out");
  };
  return (
    <div className="grid g3">
      <div className="card"><h3>Username & password</h3>
        <div className="col">
          <select value={f.provider} onChange={(e) => setF({ ...f, provider: e.target.value })}>{["local", "activeDirectory", "openLdap", "freeIpa"].map((p) => <option key={p}>{p}</option>)}</select>
          <input placeholder="username" value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} />
          <input type="password" placeholder="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} />
          <button className="btn primary" onClick={async () => { if (await act(() => api.post(`/rancher/servers/${sv.id}/login`, f), "Logged in")) onDone(); }}><LogIn size={13} />Login</button>
        </div></div>
      <div className="card"><h3>API token</h3>
        <div className="col"><input type="password" placeholder="token-xxxxx:yyyy" value={token} onChange={(e) => setToken(e.target.value)} />
          <button className="btn primary" onClick={async () => { if (await act(() => api.post("/rancher/servers", { ...sv, token }), "Token saved")) onDone(); }}>Save token</button></div></div>
      <div className="card"><h3>Single sign-on</h3>
        <div className="col"><div className="muted small">Uses any provider configured in Rancher (Azure AD, Okta, Keycloak, GitHub, SAML, OIDC). Opens your browser.</div>
          <button className="btn primary" onClick={startSSO}>Login with SSO</button><div className="small">{sso}</div></div></div>
    </div>
  );
}

function Clusters({ sv }: { sv: any }) {
  const s = useStore();
  const cs = useAsync(() => api.get(`/rancher/servers/${sv.id}/clusters`), [sv.id]);
  const [sel, setSel] = useState<any>(null);
  const [checked, setChecked] = useState<string[]>([]);
  const imp = async (ids: string[]) => { if (await act(() => api.post(`/rancher/servers/${sv.id}/import`, { clusters: ids }), "Kubeconfigs imported through Rancher auth proxy")) s.loadClusters(); };
  return (
    <div className="col">
      <div className="row"><button className="btn" onClick={cs.reload}><RefreshCw size={13} /></button><button className="btn primary" onClick={() => imp(checked)}><Download size={13} />Import {checked.length ? checked.length : "all"} kubeconfig(s)</button></div>
      <ErrorBox error={cs.error} />
      <table className="tbl"><thead><tr><th></th><th>Name</th><th>State</th><th>Provider</th><th>Version</th><th>Nodes</th><th></th></tr></thead>
        <tbody>{(cs.data || []).map((c: any) => <tr key={c.id}>
          <td><input type="checkbox" checked={checked.includes(c.id)} onChange={(e) => setChecked(e.target.checked ? [...checked, c.id] : checked.filter((x) => x !== c.id))} /></td>
          <td><b>{c.name}</b> <span className="muted small">{c.id}</span></td><td><Status s={c.state} /></td><td>{c.provider || c.driver}</td><td>{c.version}</td><td>{c.nodeCount}</td>
          <td className="row"><button className="btn sm" onClick={() => setSel(c)}>Manage</button><button className="btn sm" onClick={() => imp([c.id])}>Import</button></td></tr>)}</tbody></table>
      {sel && <ClusterManage sv={sv} c={sel} onClose={() => setSel(null)} />}
    </div>
  );
}

function ClusterManage({ sv, c, onClose }: { sv: any; c: any; onClose: () => void }) {
  const s = useStore();
  const [tab, setTab] = useState<"projects" | "nodes" | "apps">("projects");
  const projects = useAsync(() => api.get(`/rancher/servers/${sv.id}/clusters/${c.id}/projects`), [c.id]);
  const nodes = useAsync(() => (tab === "nodes" ? api.get(`/rancher/servers/${sv.id}/clusters/${c.id}/nodes`) : Promise.resolve(null)), [tab]);
  const apps = useAsync(() => (tab === "apps" ? api.get(`/rancher/servers/${sv.id}/clusters/${c.id}/apps`) : Promise.resolve(null)), [tab]);
  const [nsList, setNsList] = useState<Record<string, string[]>>({});
  const imported = s.clusters.find((x) => x.source.includes(`rancher-${sv.name}-${c.id}`));
  return (
    <div className="drawer">
      <div className="drawer-head"><b className="grow">{c.name}</b>
        {imported && <button className="btn sm" onClick={() => s.openDock({ title: `kubectl ${c.name}`, kind: "terminal", params: { clusterId: imported.id } })}><SquareTerminal size={13} />kubectl via Rancher</button>}
        <button className="btn ghost sm" onClick={onClose}>✕</button></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "projects", label: "Projects & namespaces" }, { id: "nodes", label: "Nodes" }, { id: "apps", label: "Apps & monitoring" }]} />
      <div style={{ padding: 12, overflow: "auto" }} className="grow">
        {tab === "projects" && (projects.data || []).map((p: any) => <div className="card" key={p.id} style={{ marginBottom: 8 }}>
          <div className="row"><b className="grow">{p.name}</b><span className="muted small">{p.id}</span>
            <button className="btn sm" onClick={async () => setNsList({ ...nsList, [p.id]: await api.get(`/rancher/servers/${sv.id}/clusters/${c.id}/projects/${encodeURIComponent(p.id)}/namespaces`) })}>Namespaces</button></div>
          {nsList[p.id] && <div className="row wrap" style={{ marginTop: 6 }}>{nsList[p.id].map((n) => <span key={n} className="chip" style={{ cursor: "pointer" }} onClick={() => imported && (s.selectCluster(imported.id), s.setNamespace(n), s.setView("explorer"))}>{n}</span>)}</div>}
        </div>)}
        {tab === "nodes" && <table className="tbl small"><thead><tr><th>Node</th><th>State</th><th>Roles</th><th>IP</th><th></th></tr></thead>
          <tbody>{(nodes.data?.data || []).map((n: any) => <tr key={n.id}><td>{n.nodeName || n.hostname}</td><td><Status s={n.state} /></td><td>{[n.controlPlane && "control-plane", n.etcd && "etcd", n.worker && "worker"].filter(Boolean).join(",")}</td><td>{n.ipAddress}</td>
            <td className="row">{["cordon", "uncordon", "drain"].map((a) => <button key={a} className="btn sm" onClick={() => act(() => api.post(`/rancher/servers/${sv.id}/nodes/${encodeURIComponent(n.id)}/${a}`), `${a} requested`).then(nodes.reload)}>{a}</button>)}</td></tr>)}</tbody></table>}
        {tab === "apps" && <table className="tbl small"><thead><tr><th>App</th><th>Namespace</th><th>Chart</th><th>Status</th></tr></thead>
          <tbody>{(apps.data?.data || []).map((a: any) => <tr key={a.id}><td>{a.metadata?.name}</td><td>{a.metadata?.namespace}</td><td>{a.spec?.chart?.metadata?.name} {a.spec?.chart?.metadata?.version}</td><td><Status s={a.status?.summary?.state || a.metadata?.state?.name} /></td></tr>)}</tbody></table>}
      </div>
    </div>
  );
}

function Provisioning({ sv }: { sv: any }) {
  const p = useAsync(() => api.get(`/rancher/servers/${sv.id}/provisioning`), [sv.id]);
  return (
    <div className="col">
      <ErrorBox error={p.error} />
      {(p.data?.data || []).map((c: any) => {
        const ns = c.metadata?.namespace, name = c.metadata?.name;
        return <div className="card" key={c.id}>
          <div className="row"><b className="grow">{name}</b><Badge>{c.spec?.kubernetesVersion?.includes("k3s") ? "K3s" : c.spec?.rkeConfig ? "RKE2" : "imported"}</Badge><Badge kind="info">{c.spec?.kubernetesVersion}</Badge><Status s={c.metadata?.state?.name} />
            {c.spec?.kubernetesVersion && <button className="btn sm" onClick={async () => { const v = prompt("Upgrade to Kubernetes version", c.spec.kubernetesVersion); if (v) { await act(() => api.post(`/rancher/servers/${sv.id}/provisioning/${ns}/${name}/upgrade`, { version: v }), "Upgrade started"); p.reload(); } }}>Upgrade</button>}</div>
          {(c.spec?.rkeConfig?.machinePools || []).length > 0 && <table className="tbl small" style={{ marginTop: 8 }}><thead><tr><th>Machine pool</th><th>Roles</th><th>Quantity</th><th></th></tr></thead>
            <tbody>{c.spec.rkeConfig.machinePools.map((mp: any) => <tr key={mp.name}><td>{mp.name}</td><td>{[mp.controlPlaneRole && "cp", mp.etcdRole && "etcd", mp.workerRole && "worker"].filter(Boolean).join(",")}</td><td>{mp.quantity}</td>
              <td><button className="btn sm" onClick={async () => { const q = prompt("Quantity", String(mp.quantity)); if (q) { await act(() => api.post(`/rancher/servers/${sv.id}/provisioning/${ns}/${name}/pools/${mp.name}`, { quantity: +q }), "Scaling machine pool"); p.reload(); } }}>Scale</button></td></tr>)}</tbody></table>}
        </div>;
      })}
    </div>
  );
}

function Fleet({ sv }: { sv: any }) {
  const f = useAsync(() => api.get(`/rancher/servers/${sv.id}/fleet`), [sv.id]);
  return (
    <div className="col">
      <ErrorBox error={f.error} />
      <h2>Git repos</h2>
      <table className="tbl small"><thead><tr><th>Name</th><th>Workspace</th><th>Repo</th><th>Branch</th><th>Ready clusters</th><th>Status</th></tr></thead>
        <tbody>{(f.data?.gitrepos?.data || []).map((g: any) => <tr key={g.id}><td>{g.metadata?.name}</td><td>{g.metadata?.namespace}</td><td>{g.spec?.repo}</td><td>{g.spec?.branch}</td><td>{g.status?.readyClusters}/{g.status?.desiredReadyClusters}</td><td><Status s={g.metadata?.state?.name} /></td></tr>)}</tbody></table>
      <h2>Bundles</h2>
      <table className="tbl small"><thead><tr><th>Bundle</th><th>Ready</th><th>Status</th></tr></thead>
        <tbody>{(f.data?.bundles?.data || []).map((b: any) => <tr key={b.id}><td>{b.metadata?.name}</td><td>{b.status?.summary?.ready}/{b.status?.summary?.desiredReady}</td><td><Status s={b.metadata?.state?.name} /></td></tr>)}</tbody></table>
    </div>
  );
}

function Register({ sv }: { sv: any }) {
  const [name, setName] = useState("");
  const [res, setRes] = useState<any>(null);
  return (
    <div className="col">
      <div className="row"><input placeholder="new cluster name" value={name} onChange={(e) => setName(e.target.value)} /><button className="btn primary" onClick={async () => setRes(await act(() => api.post(`/rancher/servers/${sv.id}/register`, { name }), "Cluster created in Rancher"))}>Create & get registration command</button></div>
      {res && <><div className="muted">Run on the target cluster:</div><pre>{res.command || res.insecureCommand || "Registration token pending — check Rancher."}</pre>{res.insecureCommand && <pre>{res.insecureCommand}</pre>}<JsonView data={res} /></>}
    </div>
  );
}
