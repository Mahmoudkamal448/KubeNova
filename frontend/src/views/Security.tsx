import { useMemo, useState } from "react";
import { Check, ShieldAlert, Sparkles, X } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, JsonView, Status, Tabs, useAsync } from "../components/ui";

type Tab = "rbac" | "access" | "certs" | "policies" | "images" | "scans";

export function SecurityView() {
  const s = useStore();
  const [tab, setTab] = useState<Tab>("rbac");
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster.</Empty></div>;
  return (
    <div className="page" style={{ padding: 0, display: "flex", flexDirection: "column" }}>
      <div style={{ padding: "14px 20px 0" }} className="row"><h1 className="grow"><ShieldAlert size={18} />Security</h1>
        <button className="btn ai" onClick={() => s.openAI({ task: "security", prompt: `Perform a security review of namespace ${s.namespace || "(all)"}.`, context: "" })}><Sparkles size={13} />AI security review</button></div>
      <Tabs value={tab} onChange={setTab} tabs={[{ id: "rbac", label: "RBAC explorer" }, { id: "access", label: "ServiceAccount permissions" }, { id: "certs", label: "Certificates" },
        { id: "policies", label: "Policies (Kyverno · Gatekeeper · PSA)" }, { id: "images", label: "Image scanning (Trivy)" }, { id: "scans", label: "Cluster scans (Kubescape · CIS)" }]} />
      <div className="grow" style={{ padding: 16, overflow: "auto" }}>
        {tab === "rbac" && <RBAC />}
        {tab === "access" && <Access />}
        {tab === "certs" && <Certs />}
        {tab === "policies" && <Policies />}
        {tab === "images" && <Images />}
        {tab === "scans" && <Scans />}
      </div>
    </div>
  );
}

function RBAC() {
  const s = useStore();
  const [subject, setSubject] = useState("");
  const [riskyOnly, setRiskyOnly] = useState(false);
  const g = useAsync(() => api.get(`/clusters/${s.clusterId}/security/rbac?subject=${encodeURIComponent(subject)}`), [s.clusterId, subject]);
  const rows = (g.data || []).filter((x: any) => !riskyOnly || x.risky?.length);
  const [open, setOpen] = useState<number | null>(null);
  return (
    <div className="col">
      <div className="row"><input placeholder="Filter subject (user, group, serviceaccount)…" value={subject} onChange={(e) => setSubject(e.target.value)} style={{ width: 360 }} />
        <label className="inline"><input type="checkbox" checked={riskyOnly} onChange={(e) => setRiskyOnly(e.target.checked)} />Risky grants only</label><Badge>{rows.length} grants</Badge></div>
      <ErrorBox error={g.error} />
      <table className="tbl small"><thead><tr><th>Subject</th><th>Kind</th><th>Role</th><th>Scope</th><th>Binding</th><th>Risks</th></tr></thead>
        <tbody>{rows.map((x: any, i: number) => (<>
          <tr key={i} onClick={() => setOpen(open === i ? null : i)} style={{ cursor: "pointer" }}><td><b>{x.subject.namespace ? x.subject.namespace + "/" : ""}{x.subject.name}</b></td><td>{x.subject.kind}</td><td>{x.roleKind}/{x.role}</td><td>{x.namespace || <Badge kind="warn">cluster-wide</Badge>}</td><td>{x.binding}</td>
            <td>{(x.risky || []).map((r: string) => <Badge key={r} kind="error">{r}</Badge>)}</td></tr>
          {open === i && <tr key={i + "d"}><td colSpan={6}><table className="tbl small"><thead><tr><th>API groups</th><th>Resources</th><th>Verbs</th><th>Names</th></tr></thead>
            <tbody>{(x.rules || []).map((r: any, j: number) => <tr key={j}><td>{(r.apiGroups || []).join(",") || '""'}</td><td>{(r.resources || r.nonResourceURLs || []).join(", ")}</td><td>{(r.verbs || []).join(", ")}</td><td>{(r.resourceNames || []).join(", ")}</td></tr>)}</tbody></table></td></tr>}
        </>))}</tbody></table>
    </div>
  );
}

function Access() {
  const s = useStore();
  const sas = useAsync(() => api.get(`/clusters/${s.clusterId}/r/core/v1/serviceaccounts?namespace=${s.namespace}`), [s.clusterId, s.namespace]);
  const [user, setUser] = useState("");
  const [ns, setNs] = useState(s.namespace);
  const m = useAsync(() => api.get(`/clusters/${s.clusterId}/security/access?namespace=${ns}&user=${encodeURIComponent(user)}`), [user, ns]);
  const verbs = ["get", "list", "watch", "create", "update", "patch", "delete"];
  return (
    <div className="col">
      <div className="row">
        <select value={user} onChange={(e) => setUser(e.target.value)} style={{ width: 380 }}>
          <option value="">Me (current kubeconfig identity)</option>
          {(sas.data?.rows || []).map((r: any) => <option key={r.uid} value={`system:serviceaccount:${r.namespace}:${r.name}`}>ServiceAccount {r.namespace}/{r.name}</option>)}
        </select>
        <input placeholder="or any user, e.g. jane@corp.com" onKeyDown={(e) => e.key === "Enter" && setUser((e.target as HTMLInputElement).value)} />
        <select value={ns} onChange={(e) => setNs(e.target.value)}><option value="">cluster scope</option>{s.namespaces.map((n) => <option key={n}>{n}</option>)}</select>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <table className="tbl small"><thead><tr><th>Resource</th>{verbs.map((v) => <th key={v}>{v}</th>)}</tr></thead>
        <tbody>{Object.entries(m.data).map(([r, vs]: any) => <tr key={r}><td>{r}</td>{verbs.map((v) => <td key={v}>{v in vs ? (vs[v] ? <Check size={14} color="var(--ok)" /> : <X size={14} color="var(--fg3)" />) : ""}</td>)}</tr>)}</tbody></table>}
    </div>
  );
}

function Certs() {
  const s = useStore();
  const c = useAsync(() => api.get(`/clusters/${s.clusterId}/security/certificates`), [s.clusterId]);
  return (
    <>
      <ErrorBox error={c.error} />
      <table className="tbl"><thead><tr><th>Status</th><th>Days left</th><th>Source</th><th>Name</th><th>Namespace</th><th>Expires</th></tr></thead>
        <tbody>{(c.data || []).map((x: any, i: number) => <tr key={i}><td><Badge kind={x.status === "ok" ? "ok" : x.status === "warning" ? "warn" : "error"}>{x.status}</Badge></td><td>{x.daysLeft}</td><td>{x.source}</td><td>{x.name}{x.key ? ` (${x.key})` : ""}</td><td>{x.namespace}</td><td>{x.notAfter?.slice(0, 10)}</td></tr>)}</tbody></table>
      {c.data && !c.data.length && <Empty>No certificates found.</Empty>}
    </>
  );
}

function Policies() {
  const s = useStore();
  const p = useAsync(() => api.get(`/clusters/${s.clusterId}/security/policies`), [s.clusterId]);
  const by = useMemo(() => ({ kyverno: (p.data || []).filter((x: any) => x.engine === "kyverno"), gatekeeper: (p.data || []).filter((x: any) => x.engine === "gatekeeper"), psa: (p.data || []).filter((x: any) => x.engine === "psa") }), [p.data]);
  return (
    <div className="col">
      <ErrorBox error={p.error} />
      <h2>Kyverno</h2>
      {by.kyverno.length ? <table className="tbl small"><thead><tr><th>Policy</th><th>Kind</th><th>Action</th><th>Ready</th><th>Violations</th></tr></thead>
        <tbody>{by.kyverno.map((x: any) => <tr key={x.name}><td>{x.namespace ? x.namespace + "/" : ""}{x.name}</td><td>{x.kind}</td><td>{x.action}</td><td><Status s={x.ready ? "Ready" : "NotReady"} /></td><td>{x.violations ? <Badge kind="error">{x.violations}</Badge> : 0}</td></tr>)}</tbody></table> : <div className="muted">Kyverno not installed.</div>}
      <h2>OPA Gatekeeper</h2>
      {by.gatekeeper.length ? <table className="tbl small"><thead><tr><th>Constraint</th><th>Template</th><th>Enforcement</th><th>Violations</th></tr></thead>
        <tbody>{by.gatekeeper.map((x: any) => <tr key={x.kind + x.name}><td>{x.name}</td><td>{x.kind}</td><td>{x.action}</td><td>{x.violations ? <Badge kind="error">{x.violations}</Badge> : 0}</td></tr>)}</tbody></table> : <div className="muted">Gatekeeper not installed.</div>}
      <h2>Pod Security Admission</h2>
      <table className="tbl small"><thead><tr><th>Namespace</th><th>Levels</th></tr></thead>
        <tbody>{by.psa.map((x: any) => <tr key={x.name}><td>{x.name}</td><td>{x.action.includes("privileged (default)") ? <Badge kind="warn">{x.action}</Badge> : x.action}</td></tr>)}</tbody></table>
    </div>
  );
}

function Images() {
  const s = useStore();
  const imgs = useAsync(() => api.get(`/clusters/${s.clusterId}/security/images`), [s.clusterId]);
  const [reports, setReports] = useState<Record<string, any>>({});
  const [busy, setBusy] = useState("");
  const scan = async (image: string) => {
    setBusy(image);
    const r = await act(() => api.post("/security/scan-image", { image }));
    setBusy("");
    if (r) setReports((x) => ({ ...x, [image]: r }));
  };
  const summary = (r: any) => {
    const c: Record<string, number> = {};
    for (const res of r?.Results || []) for (const v of res.Vulnerabilities || []) c[v.Severity] = (c[v.Severity] || 0) + 1;
    return c;
  };
  return (
    <div className="col">
      <div className="muted small">Scans use Trivy (bundled or on PATH). In air-gapped mode point Trivy at an offline DB.</div>
      <ErrorBox error={imgs.error} />
      <table className="tbl small"><thead><tr><th>Image</th><th>Used by</th><th>Vulnerabilities</th><th></th></tr></thead>
        <tbody>{Object.entries(imgs.data || {}).map(([img, pods]: any) => {
          const sm = summary(reports[img]);
          return <tr key={img}><td className="mono">{img}{img.endsWith(":latest") || !img.includes(":") ? <Badge kind="warn">:latest</Badge> : null}</td><td>{pods.length} pods</td>
            <td>{reports[img] ? ["CRITICAL", "HIGH", "MEDIUM", "LOW"].map((k) => sm[k] ? <Badge key={k} kind={k === "CRITICAL" || k === "HIGH" ? "error" : "warn"}>{k[0]}:{sm[k]}</Badge> : null) : ""}</td>
            <td><button className="btn sm" disabled={!!busy} onClick={() => scan(img)}>{busy === img ? "Scanning…" : "Scan"}</button></td></tr>;
        })}</tbody></table>
      {Object.entries(reports).map(([img, r]) => <details key={img}><summary>{img}</summary>
        <table className="tbl small"><thead><tr><th>ID</th><th>Severity</th><th>Package</th><th>Installed</th><th>Fixed</th><th>Title</th></tr></thead>
          <tbody>{(r.Results || []).flatMap((x: any) => x.Vulnerabilities || []).slice(0, 500).map((v: any, i: number) => <tr key={i}><td>{v.VulnerabilityID}</td><td><Badge kind={v.Severity === "CRITICAL" || v.Severity === "HIGH" ? "error" : "warn"}>{v.Severity}</Badge></td><td>{v.PkgName}</td><td>{v.InstalledVersion}</td><td>{v.FixedVersion}</td><td style={{ whiteSpace: "normal" }}>{v.Title}</td></tr>)}</tbody></table></details>)}
    </div>
  );
}

function Scans() {
  const s = useStore();
  const [res, setRes] = useState<any>(null);
  const [busy, setBusy] = useState(false);
  const run = async (scanner: string, framework = "") => {
    setBusy(true);
    setRes(await act(() => api.post(`/clusters/${s.clusterId}/security/scan`, { scanner, framework })));
    setBusy(false);
  };
  const controls = res?.summaryDetails?.controls ? Object.values(res.summaryDetails.controls) as any[] : null;
  return (
    <div className="col">
      <div className="row wrap">
        <button className="btn" disabled={busy} onClick={() => run("kubescape", "nsa")}>Kubescape · NSA</button>
        <button className="btn" disabled={busy} onClick={() => run("kubescape", "mitre")}>Kubescape · MITRE</button>
        <button className="btn" disabled={busy} onClick={() => run("cis")}>CIS Benchmark</button>
        <button className="btn" disabled={busy} onClick={() => run("trivy")}>Trivy k8s</button>
        {busy && <span className="muted">Scanning… this can take several minutes</span>}
      </div>
      {res?.summaryDetails && <div className="row"><Badge kind="info">Compliance score: {Math.round(res.summaryDetails.complianceScore ?? res.summaryDetails.score ?? 0)}%</Badge></div>}
      {controls ? <table className="tbl small"><thead><tr><th>Control</th><th>Status</th><th>Score</th><th>Failed resources</th></tr></thead>
        <tbody>{controls.sort((a, b) => (a.score ?? 100) - (b.score ?? 100)).map((c: any) => <tr key={c.controlID}><td>{c.controlID} {c.name}</td><td><Status s={c.status?.status || c.statusInfo?.status} /></td><td>{Math.round(c.score ?? 0)}</td><td>{c.ResourceCounters?.failedResources ?? c.resourceCounters?.failedResources}</td></tr>)}</tbody></table>
        : res && <JsonView data={res} />}
    </div>
  );
}
