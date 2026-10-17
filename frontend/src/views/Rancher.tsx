import { useEffect, useRef, useState } from "react";
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

const providerLabel: Record<string, string> = {
  localProvider: "Local", activeDirectoryProvider: "Active Directory", openLdapProvider: "OpenLDAP", freeIpaProvider: "FreeIPA",
  keyCloakOIDCProvider: "Keycloak (OIDC)", genericOIDCProvider: "OIDC", oidcProvider: "OIDC", cognitoProvider: "Amazon Cognito",
  azureADProvider: "Azure AD / Entra ID", githubProvider: "GitHub", githubAppProvider: "GitHub App", googleOAuthProvider: "Google",
  oktaProvider: "Okta (SAML)", keyCloakProvider: "Keycloak (SAML)", adfsProvider: "ADFS", pingProvider: "Ping", shibbolethProvider: "Shibboleth",
};

function Login({ sv, onDone }: { sv: any; onDone: () => void }) {
  const providers = useAsync(() => api.get(`/rancher/servers/${sv.id}/providers`), [sv.id]);
  const pw = (providers.data || []).filter((p: any) => p.login === "password");
  const sso = (providers.data || []).filter((p: any) => p.login !== "password");
  const hasSaml = sso.some((p: any) => p.login === "browser");
  const samlOnly = hasSaml && sso.every((p: any) => p.login === "browser");
  const base = sv.url.replace(/\/$/, "");
  const [f, setF] = useState({ provider: "localProvider", username: "", password: "" });
  const [token, setToken] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const stop = useRef(false);
  useEffect(() => () => { stop.current = true; }, []);
  useEffect(() => { if (pw.length && !pw.some((p: any) => p.type === f.provider)) setF((x) => ({ ...x, provider: pw[0].type })); }, [providers.data]);

  const saveToken = async (t: string) => {
    const r = await act(() => api.post(`/rancher/servers/${sv.id}/token`, { token: t }), "Logged in to Rancher");
    if (r) onDone();
    return !!r;
  };

  // Rancher's own login page in a KubeNova window: works with every provider.
  const windowLogin = async () => {
    setBusy(true);
    setStatus("Log in in the Rancher window…");
    try {
      const r = await window.kubenova!.rancherLogin!({ url: sv.url, insecure: sv.insecure });
      if (r.cancelled) setStatus("Login window closed before logging in.");
      else if (r.token) { setStatus("Checking token…"); if (await saveToken(r.token)) setStatus(""); else setStatus("Rancher rejected the session token."); }
    } finally { setBusy(false); }
  };

  // Real SSO in the user's own Chrome / Edge / Brave (desktop app).
  const ssoLogin = async () => {
    stop.current = false;
    setBusy(true);
    setStatus("Log in to Rancher in the browser window that just opened. KubeNova continues automatically.");
    try {
      const r = await window.kubenova!.rancherBrowserLogin!({ url: sv.url, serverId: sv.id });
      if (r.ok) { setStatus(""); useStore.getState().notify("Logged in to Rancher", "ok"); onDone(); return; }
      if (r.noBrowser) { setBusy(false); setStatus("No Chrome, Edge, Brave or Chromium found. Using an API key instead…"); return keyLogin(); }
      setStatus(r.error ? `Login failed: ${r.error}` : stop.current ? "" : "The browser was closed before logging in.");
    } finally { setBusy(false); }
  };

  // Your own browser + a Rancher API key: works with every provider, and with
  // Cloudflare or other bot checks in front of Rancher. Rancher can't hand an
  // OIDC login back to an app, so the user creates a key and copies it;
  // KubeNova picks it up from the clipboard.
  const keyLogin = async () => {
    openExternal(`${base}/dashboard/account/create-key`);
    const read = window.kubenova?.rancherClipboard;
    if (!read) { setStatus("After creating the key in your browser, paste its bearer token under API token."); return; }
    stop.current = false;
    setBusy(true);
    setStatus("In your browser: log in, click Create, then click the copy button next to the Bearer Token. KubeNova picks it up automatically.");
    const seen = new Set([await read()]);
    let access = "";
    const deadline = Date.now() + 15 * 60 * 1000;
    while (!stop.current && Date.now() < deadline) {
      await new Promise((res) => setTimeout(res, 1000));
      const t = await read();
      if (!t || seen.has(t)) continue;
      seen.add(t);
      let full = t.includes(":") ? t : "";
      if (!full && t.startsWith("token-")) { access = t; setStatus("Got the access key. Now copy the secret key (or the bearer token)."); continue; }
      if (!full && access) full = `${access}:${t}`;
      if (!full) continue;
      setStatus("Checking token…");
      if (await saveToken(full)) { setBusy(false); setStatus(""); return; }
      setStatus("Rancher rejected that token. Copy the Bearer Token again.");
    }
    setBusy(false);
    if (!stop.current) setStatus("Stopped waiting. Paste the bearer token under API token.");
  };

  // Rancher's own browser token flow; fully automatic but SAML providers only.
  const browserLogin = async () => {
    const r = await act(() => api.post(`/rancher/servers/${sv.id}/sso`));
    if (!r) return;
    openExternal(r.url);
    stop.current = false;
    setBusy(true);
    setStatus("Waiting for you to finish logging in in the browser…");
    const deadline = Date.now() + 15 * 60 * 1000;
    while (!stop.current && Date.now() < deadline) {
      await new Promise((res) => setTimeout(res, 3000));
      try {
        const p = await api.get(`/rancher/servers/${sv.id}/sso/${r.requestId}`);
        if (p.done) { setBusy(false); setStatus(""); useStore.getState().notify("Logged in to Rancher", "ok"); onDone(); return; }
      } catch (e: any) { setBusy(false); setStatus(`Login failed: ${e.message}`); return; }
    }
    setBusy(false);
    if (!stop.current) setStatus("Timed out. If your Rancher uses OIDC, GitHub or Azure AD, use “Log in with your browser” without SAML, or an API token.");
  };

  return (
    <div className="col">
      <ErrorBox error={providers.error} />
      {providers.data && <div className="row wrap small"><span className="muted">This Rancher offers:</span>{providers.data.map((p: any) => <span key={p.type} className="chip">{providerLabel[p.type] || p.name || p.type}</span>)}</div>}
      <div className="grid g3">
        <div className="card col"><h3>Single sign-on</h3>
          {window.kubenova?.rancherBrowserLogin ? <>
            <div className="muted small">Opens Rancher in Chrome, Edge or Brave (its own KubeNova profile). Log in with {sso.length ? sso.map((p: any) => providerLabel[p.type] || p.type).join(" / ") : "your SSO provider"} as usual; KubeNova picks up the session automatically. Works behind Cloudflare.</div>
            <button className="btn primary" disabled={busy} onClick={ssoLogin}><LogIn size={13} />Log in with your browser</button>
          </> : <>
            <div className="muted small">{samlOnly
              ? "Opens your browser; log in there and KubeNova is logged in automatically."
              : "Opens Rancher in your own browser. Log in, click Create, then copy the Bearer Token; KubeNova picks it up from the clipboard."}</div>
            <button className="btn primary" disabled={busy} onClick={samlOnly ? browserLogin : keyLogin}><LogIn size={13} />Log in with your browser</button>
          </>}
          {hasSaml && !samlOnly && <button className="btn" disabled={busy} onClick={browserLogin}>SAML login (automatic)</button>}
          {window.kubenova?.rancherLogin && <button className="btn ghost" disabled={busy} onClick={windowLogin}>Log in in a KubeNova window instead</button>}
          {busy && <button className="btn ghost sm" onClick={() => { stop.current = true; window.kubenova?.rancherBrowserLoginCancel?.(); setBusy(false); setStatus(""); }}>Stop waiting</button>}
          {status && <div className="small">{status}</div>}
        </div>
        <div className="card col"><h3>Username & password</h3>
          {pw.length ? <>
            <select value={f.provider} onChange={(e) => setF({ ...f, provider: e.target.value })}>{pw.map((p: any) => <option key={p.type} value={p.type}>{providerLabel[p.type] || p.type}</option>)}</select>
            <input placeholder="username" value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} />
            <input type="password" placeholder="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} onKeyDown={(e) => e.key === "Enter" && (document.getElementById("rancher-pw-login") as HTMLButtonElement)?.click()} />
            <button id="rancher-pw-login" className="btn" onClick={async () => { if (await act(() => api.post(`/rancher/servers/${sv.id}/login`, f), "Logged in to Rancher")) onDone(); }}><LogIn size={13} />Login</button>
          </> : <div className="muted small">{providers.loading ? "Loading…" : "No username/password provider is enabled on this Rancher."}</div>}
        </div>
        <div className="card col"><h3>API token</h3>
          <div className="muted small">In Rancher: your avatar → <b>Account & API Keys</b> → <b>Create API Key</b>, then paste the bearer token.</div>
          <input type="password" placeholder="token-xxxxx:yyyy" value={token} onChange={(e) => setToken(e.target.value)} />
          <div className="row"><button className="btn" disabled={!token} onClick={() => saveToken(token)}>Save token</button>
            <button className="btn ghost" onClick={() => openExternal(`${base}/dashboard/account/create-key`)}>Create a key in Rancher</button></div>
        </div>
      </div>
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
  // Must match the file name the backend gives imported kubeconfigs (/, \\, : and spaces become "-").
  const importedName = `rancher-${sv.name}-${c.id}`.replace(/[/\\: ]/g, "-");
  const imported = s.clusters.find((x) => x.source.replace(/\\/g, "/").split("/").pop() === importedName + ".yaml");
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
