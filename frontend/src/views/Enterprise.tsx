import { useState } from "react";
import { Building2, Download, LogIn, LogOut, Plus, ShieldCheck, Trash2, Users } from "lucide-react";
import { api, downloadUrl, openExternal } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Field, Modal, Tabs, useAsync } from "../components/ui";

export function EnterpriseView() {
  const [tab, setTab] = useState<"license" | "workspaces" | "audit" | "identity">("license");
  return (
    <div className="page" style={{ padding: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "14px 20px 0" }}><h1><Building2 size={18} />Teams & Enterprise <Badge kind="pro">All features unlocked</Badge></h1></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "license", label: "Edition & features" }, { id: "workspaces", label: "Team workspaces & RBAC" }, { id: "audit", label: "Audit logs" }, { id: "identity", label: "SSO · SAML · OIDC · LDAP · SCIM" }]} />
      <div className="grow" style={{ padding: 16, overflow: "auto" }}>
        {tab === "license" && <License />}
        {tab === "workspaces" && <Workspaces />}
        {tab === "audit" && <Audit />}
        {tab === "identity" && <Identity />}
      </div>
    </div>
  );
}

function License() {
  const l = useAsync(() => api.get("/license"), []);
  const [f, setF] = useState({ licensee: "", key: "" });
  const d = l.data;
  if (!d) return <ErrorBox error={l.error} />;
  return (
    <div className="col">
      <div className="card" style={{ borderColor: "var(--accent)" }}>
        <div className="row"><ShieldCheck size={22} color="var(--ok)" /><div className="grow"><b style={{ fontSize: 16 }}>{d.edition}</b>
          <div className="muted">Every Pro and Enterprise capability — team workspaces, RBAC, audit logs, SSO (SAML/OIDC/LDAP/SCIM), air-gapped mode, FIPS, unlimited clusters and seats — is available in the free edition. No license key is required.</div></div></div>
        <div className="row wrap" style={{ marginTop: 10 }}><Badge kind="ok">Seats: unlimited</Badge><Badge kind={d.fips ? "ok" : ""}>FIPS 140-3 mode: {d.fips ? "active" : "off (build with KUBENOVA_FIPS=1)"}</Badge>{d.licensee && <Badge>Registered to {d.licensee}</Badge>}</div>
      </div>
      <h2>Included features ({Object.keys(d.features).length})</h2>
      <div className="row wrap">{Object.keys(d.features).sort().map((f) => <Badge key={f} kind="ok">✓ {f}</Badge>)}</div>
      <h2>Optional registration (support contact only)</h2>
      <div className="row"><input placeholder="Organisation" value={f.licensee} onChange={(e) => setF({ ...f, licensee: e.target.value })} /><input placeholder="Support key (optional)" value={f.key} onChange={(e) => setF({ ...f, key: e.target.value })} />
        <button className="btn" onClick={async () => { await act(() => api.post("/license", f), "Saved"); l.reload(); }}>Save</button></div>
    </div>
  );
}

function Workspaces() {
  const s = useStore();
  const ws = useAsync(() => api.get("/workspaces"), []);
  const scim = useAsync(() => api.get("/scim/users"), []);
  const [edit, setEdit] = useState<any>(null);
  return (
    <div className="col">
      <div className="row"><label className="inline"><input type="checkbox" checked={!!s.settings?.appRbac} onChange={async (e) => { const cur = await api.get("/settings"); const on = e.target.checked; await act(() => api.put("/settings", { ...cur, appRbac: on }), on ? "Workspace RBAC enforced" : "Workspace RBAC disabled"); s.loadSettings(); }} />Enforce workspace RBAC (viewer = read-only, editor = changes, admin/owner = manage)</label>
        <div className="grow" /><button className="btn primary" onClick={() => setEdit({ name: "", clusterIds: [], members: [] })}><Plus size={13} />New workspace</button>
        <a className="btn" href={"data:application/json," + encodeURIComponent(JSON.stringify(ws.data || [], null, 2))} download="kubenova-workspaces.json"><Download size={13} />Export</a></div>
      {(ws.data || []).map((w: any) => (
        <div className="card" key={w.id}>
          <div className="row"><Users size={14} /><b className="grow">{w.name}</b><Badge>{w.clusterIds?.length || 0} clusters</Badge><Badge>{w.members?.length || 0} members</Badge>
            <button className="btn sm" onClick={() => setEdit(w)}>Edit</button><button className="btn sm danger" onClick={async () => { await api.del(`/workspaces/${w.id}`); ws.reload(); }}><Trash2 size={12} /></button></div>
          <div className="small muted">{w.description}</div>
          <div className="row wrap small" style={{ marginTop: 6 }}>{(w.members || []).map((m: any) => <span key={m.subject} className="chip">{m.subject} · {m.role}</span>)}</div>
        </div>
      ))}
      {ws.data && !ws.data.length && <Empty>Create a workspace to share clusters with a team and scope permissions.</Empty>}
      {edit && <Modal title="Workspace" onClose={() => setEdit(null)} footer={<button className="btn primary" onClick={async () => { if (await act(() => api.post("/workspaces", edit), "Saved")) { setEdit(null); ws.reload(); } }}>Save</button>}>
        <div className="grid g2"><Field label="Name"><input value={edit.name} onChange={(e) => setEdit({ ...edit, name: e.target.value })} /></Field><Field label="Description"><input value={edit.description || ""} onChange={(e) => setEdit({ ...edit, description: e.target.value })} /></Field></div>
        <h2>Clusters</h2>
        <div className="row wrap">{s.clusters.map((c) => <label key={c.id} className="inline"><input type="checkbox" checked={edit.clusterIds?.includes(c.id)} onChange={(e) => setEdit({ ...edit, clusterIds: e.target.checked ? [...(edit.clusterIds || []), c.id] : edit.clusterIds.filter((x: string) => x !== c.id) })} />{c.name}</label>)}</div>
        <h2>Members</h2>
        {(edit.members || []).map((m: any, i: number) => <div key={i} className="row" style={{ marginBottom: 4 }}>
          <input className="grow" list="scim-users" value={m.subject} placeholder="email, username or group:<name>" onChange={(e) => setEdit({ ...edit, members: edit.members.map((x: any, j: number) => (j === i ? { ...x, subject: e.target.value } : x)) })} />
          <select value={m.role} onChange={(e) => setEdit({ ...edit, members: edit.members.map((x: any, j: number) => (j === i ? { ...x, role: e.target.value } : x)) })}>{["viewer", "editor", "admin", "owner"].map((r) => <option key={r}>{r}</option>)}</select>
          <button className="btn ghost sm" onClick={() => setEdit({ ...edit, members: edit.members.filter((_: any, j: number) => j !== i) })}><Trash2 size={12} /></button></div>)}
        <datalist id="scim-users">{(scim.data || []).map((u: any) => <option key={u.id} value={u.userName || u.emails?.[0]?.value} />)}</datalist>
        <button className="btn sm" onClick={() => setEdit({ ...edit, members: [...(edit.members || []), { subject: "", role: "viewer" }] })}><Plus size={12} />Member</button>
      </Modal>}
    </div>
  );
}

function Audit() {
  const [q, setQ] = useState("");
  const a = useAsync(() => api.get(`/audit?limit=1000&q=${encodeURIComponent(q)}`), [q]);
  return (
    <div className="col">
      <div className="row"><input className="grow" placeholder="Search audit log (user, cluster, path, action)…" value={q} onChange={(e) => setQ(e.target.value)} /><a className="btn" href={downloadUrl("/audit/export")}><Download size={13} />Export JSONL (SIEM)</a></div>
      <table className="tbl small"><thead><tr><th>Time</th><th>User</th><th>Action</th><th>Method</th><th>Path</th><th>Status</th></tr></thead>
        <tbody>{(a.data || []).map((e: any, i: number) => <tr key={i}><td>{new Date(e.time).toLocaleString()}</td><td>{e.user}</td><td>{e.action}</td><td>{e.method}</td><td className="mono">{e.path}</td><td><Badge kind={e.status >= 400 ? "error" : "ok"}>{e.status || "-"}</Badge></td></tr>)}</tbody></table>
    </div>
  );
}

function Identity() {
  const me = useAsync(() => api.get("/auth/me"), []);
  const cfg = useAsync(() => api.get("/auth/config"), []);
  const [c, setC] = useState<any>(null);
  const [ldap, setLdap] = useState({ username: "", password: "" });
  const conf = c || cfg.data;
  if (!conf) return <ErrorBox error={cfg.error} />;
  const set = (path: string[], v: any) => { const n = JSON.parse(JSON.stringify(conf)); let o = n; for (const p of path.slice(0, -1)) o = o[p] ||= {}; o[path[path.length - 1]] = v; setC(n); };
  return (
    <div className="col">
      <div className="card row"><b className="grow">Signed in as {me.data?.name} {me.data?.email && `<${me.data.email}>`}</b><Badge>{me.data?.provider}</Badge>{(me.data?.groups || []).map((g: string) => <span key={g} className="chip">{g}</span>)}
        <button className="btn" onClick={async () => { await api.post("/auth/logout"); me.reload(); }}><LogOut size={13} />Sign out</button></div>
      <div className="grid g3">
        <div className="card col"><h3>OIDC (Okta, Entra ID, Keycloak, Google, Auth0)</h3>
          <Field label="Issuer"><input value={conf.oidc?.issuer || ""} onChange={(e) => set(["oidc", "issuer"], e.target.value)} /></Field>
          <Field label="Client ID"><input value={conf.oidc?.clientId || ""} onChange={(e) => set(["oidc", "clientId"], e.target.value)} /></Field>
          <Field label="Client secret (optional with PKCE)"><input type="password" value={conf.oidc?.clientSecret || ""} onChange={(e) => set(["oidc", "clientSecret"], e.target.value)} /></Field>
          <Field label="Groups claim"><input value={conf.oidc?.groupsClaim || ""} placeholder="groups" onChange={(e) => set(["oidc", "groupsClaim"], e.target.value)} /></Field>
          <button className="btn" onClick={async () => { const r = await act(() => api.post("/auth/oidc/start")); if (r) openExternal(r.url); }}><LogIn size={13} />Sign in with OIDC</button></div>
        <div className="card col"><h3>SAML 2.0 (ADFS, Entra ID, Okta, PingFederate)</h3>
          <Field label="IdP metadata URL"><input value={conf.saml?.idpMetadataUrl || ""} onChange={(e) => set(["saml", "idpMetadataUrl"], e.target.value)} /></Field>
          <Field label="SP entity ID (optional)"><input value={conf.saml?.entityId || ""} onChange={(e) => set(["saml", "entityId"], e.target.value)} /></Field>
          <div className="muted small">SP metadata: <span className="mono">/api/auth/saml/saml/metadata</span> · ACS: <span className="mono">/api/auth/saml/saml/acs</span></div>
          <button className="btn" onClick={() => openExternal(downloadUrl("/auth/saml/login").replace(/[?&]token=[^&]+/, ""))}><LogIn size={13} />Sign in with SAML</button></div>
        <div className="card col"><h3>LDAP / Active Directory</h3>
          <Field label="URL"><input value={conf.ldap?.url || ""} placeholder="ldaps://dc.corp.local:636" onChange={(e) => set(["ldap", "url"], e.target.value)} /></Field>
          <Field label="Bind DN"><input value={conf.ldap?.bindDn || ""} onChange={(e) => set(["ldap", "bindDn"], e.target.value)} /></Field>
          <Field label="Bind password"><input type="password" value={conf.ldap?.bindPassword || ""} onChange={(e) => set(["ldap", "bindPassword"], e.target.value)} /></Field>
          <Field label="Base DN"><input value={conf.ldap?.baseDn || ""} onChange={(e) => set(["ldap", "baseDn"], e.target.value)} /></Field>
          <Field label="User filter"><input value={conf.ldap?.userFilter || ""} placeholder="(sAMAccountName=%s)" onChange={(e) => set(["ldap", "userFilter"], e.target.value)} /></Field>
          <div className="row"><input placeholder="username" value={ldap.username} onChange={(e) => setLdap({ ...ldap, username: e.target.value })} /><input type="password" placeholder="password" value={ldap.password} onChange={(e) => setLdap({ ...ldap, password: e.target.value })} />
            <button className="btn" onClick={async () => { await act(() => api.post("/auth/ldap", ldap), "Signed in"); me.reload(); }}>Sign in</button></div></div>
      </div>
      <div className="card col"><h3>SCIM 2.0 provisioning</h3>
        <div className="muted small">Point your IdP at <span className="mono">{"<server>"}/scim/v2</span> with this bearer token to provision users & groups for workspace membership.</div>
        <div className="row"><input type="password" className="grow" value={conf.scimToken || ""} onChange={(e) => set(["scimToken"], e.target.value)} /><button className="btn" onClick={() => set(["scimToken"], crypto.randomUUID().replace(/-/g, "") + crypto.randomUUID().replace(/-/g, ""))}>Generate</button></div></div>
      <button className="btn primary" style={{ alignSelf: "start" }} disabled={!c} onClick={async () => { if (await act(() => api.put("/auth/config", c), "Identity settings saved")) { setC(null); cfg.reload(); } }}>Save identity settings</button>
    </div>
  );
}
