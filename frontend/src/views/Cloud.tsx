import { useState } from "react";
import { Cloud as CloudIcon, Download, LogIn } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, Field, JsonView, Status, useAsync } from "../components/ui";

const providers = [
  { id: "aws", name: "AWS", sub: "EKS · IAM · SSO · CloudWatch · ELB · EBS · EFS · Route53", fields: ["profile", "region"], login: true },
  { id: "azure", name: "Azure", sub: "AKS · Azure AD · Managed Identity · Monitor · Disk · Files", fields: ["subscription"], login: true },
  { id: "gcp", name: "Google Cloud", sub: "GKE · Cloud Logging · Monitoring · GCR · Artifact Registry", fields: ["project"], login: true },
  { id: "digitalocean", name: "DigitalOcean", sub: "DOKS · Load Balancers · Block Storage · Spaces (via S3 browser)", fields: [] },
  { id: "oracle", name: "Oracle Cloud", sub: "OKE", fields: ["compartment", "region"] },
  { id: "ibm", name: "IBM Cloud", sub: "IKS / ROKS", fields: [] },
  { id: "linode", name: "Linode / Akamai", sub: "LKE", fields: [] },
  { id: "vultr", name: "Vultr", sub: "VKE", fields: [] },
];

export function CloudView() {
  const [p, setP] = useState(providers[0]);
  return (
    <div className="split" style={{ flex: 1 }}>
      <div className="side">
        <div className="sec">Providers</div>
        {providers.map((x) => <div key={x.id} className={`item ${x.id === p.id ? "active" : ""}`} onClick={() => setP(x)}>{x.name}</div>)}
      </div>
      <div className="page"><Provider key={p.id} p={p} /></div>
    </div>
  );
}

function Provider({ p }: { p: (typeof providers)[number] }) {
  const s = useStore();
  const [opts, setOpts] = useState<Record<string, string>>({});
  const [clusters, setClusters] = useState<any[] | null>(null);
  const [busy, setBusy] = useState(false);
  const profiles = useAsync(() => (p.login ? api.get(`/cloud/${p.id}/profiles`) : Promise.resolve([])), [p.id]);
  const kinds = useAsync(() => api.get("/cloud/kinds"), []);
  const [res, setRes] = useState<any>(null);
  const discover = async () => { setBusy(true); setClusters((await act(() => api.post(`/cloud/${p.id}/discover`, opts))) || null); setBusy(false); };
  return (
    <>
      <h1><CloudIcon size={18} />{p.name}</h1>
      <div className="muted" style={{ marginBottom: 12 }}>{p.sub}. Uses your existing CLI credentials; imported kubeconfigs use the provider's exec auth plugin (short-lived tokens).</div>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        {p.fields.map((f) => <Field key={f} label={f}>
          {f === "profile" || f === "subscription" || f === "project" ? <><input list={`${p.id}-${f}`} value={opts[f] || ""} onChange={(e) => setOpts({ ...opts, [f]: e.target.value })} /><datalist id={`${p.id}-${f}`}>{(profiles.data || []).map((x: string) => <option key={x} value={x} />)}</datalist></>
            : <input value={opts[f] || ""} onChange={(e) => setOpts({ ...opts, [f]: e.target.value })} />}
        </Field>)}
        {p.login && <button className="btn" onClick={() => act(() => api.post(`/cloud/${p.id}/login`, { ...opts, method: "" }), "Login complete")}><LogIn size={13} />{p.id === "aws" ? "AWS SSO login" : p.id === "azure" ? "Azure AD login" : "gcloud login"}</button>}
        {p.id === "azure" && <button className="btn" onClick={() => act(() => api.post(`/cloud/azure/login`, { method: "managed-identity" }), "Logged in with managed identity")}>Managed Identity</button>}
        <button className="btn primary" disabled={busy} onClick={discover}>{busy ? "Discovering…" : "Discover clusters"}</button>
      </div>
      {clusters && (clusters.length ? <table className="tbl"><thead><tr><th>Name</th><th>Location</th><th>Version</th><th>Status</th><th>Account</th><th></th></tr></thead>
        <tbody>{clusters.map((c) => <tr key={c.id}><td><b>{c.name}</b></td><td>{c.location}</td><td>{c.version}</td><td><Status s={c.status} /></td><td>{c.account || c.group}</td>
          <td><button className="btn sm primary" onClick={async () => { if (await act(() => api.post(`/cloud/${p.id}/import`, { cluster: c, options: opts }), `Imported ${c.name}`)) s.loadClusters(); }}><Download size={12} />Import</button></td></tr>)}</tbody></table> : <Empty>No clusters found.</Empty>)}
      {kinds.data?.[p.id] && <>
        <h2>Cloud resources</h2>
        <div className="row wrap">{kinds.data[p.id].map((k: string) => <button key={k} className="btn sm" onClick={async () => setRes({ k, data: await act(() => api.post(`/cloud/${p.id}/resources/${k}`, opts)) })}>{k}</button>)}</div>
        {res && <><Badge>{res.k}</Badge><JsonView data={res.data} /></>}
      </>}
    </>
  );
}
