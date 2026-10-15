import { useState } from "react";
import { Database, Folder, FolderOpen, GitBranch, HardDrive, KeyRound, Plug, Plus, Server, SquareTerminal, Trash2, Wallet, Archive, FileCode, Boxes, Play, Network } from "lucide-react";
import { api, downloadUrl } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, ErrorBox, Field, JsonView, Modal, Status, useAsync } from "../components/ui";
import { YamlEditor } from "../components/YamlEditor";
import { bytes } from "../util";

type Tool = "ssh" | "db" | "s3" | "http" | "terraform" | "ansible" | "git" | "files" | "cost" | "backup" | "forwards";
const tools: { id: Tool; label: string; icon: JSX.Element }[] = [
  { id: "ssh", label: "SSH manager", icon: <Server size={14} /> }, { id: "db", label: "Databases", icon: <Database size={14} /> },
  { id: "s3", label: "S3 browser", icon: <HardDrive size={14} /> }, { id: "http", label: "Harbor · Vault · CI", icon: <KeyRound size={14} /> },
  { id: "terraform", label: "Terraform state", icon: <Boxes size={14} /> }, { id: "ansible", label: "Ansible", icon: <Play size={14} /> },
  { id: "git", label: "Git", icon: <GitBranch size={14} /> }, { id: "files", label: "File explorer", icon: <FolderOpen size={14} /> },
  { id: "cost", label: "Cost dashboard", icon: <Wallet size={14} /> }, { id: "backup", label: "Backup / restore", icon: <Archive size={14} /> },
  { id: "forwards", label: "Port forwards", icon: <Network size={14} /> },
];

export function ToolboxView() {
  const [t, setT] = useState<Tool>("ssh");
  return (
    <div className="split" style={{ flex: 1 }}>
      <div className="side"><div className="sec">Toolbox</div>
        {tools.map((x) => <div key={x.id} className={`item ${x.id === t ? "active" : ""}`} onClick={() => setT(x.id)}><span className="row">{x.icon}{x.label}</span></div>)}</div>
      <div className="page">
        {t === "ssh" && <Connections kinds={["ssh"]} title="SSH manager" />}
        {t === "db" && <Connections kinds={["postgres", "mysql", "redis", "mongodb"]} title="Database manager" />}
        {t === "s3" && <S3 />}
        {t === "http" && <Connections kinds={["harbor", "vault", "jenkins", "gitlab", "github"]} title="Harbor · Vault · Jenkins · GitLab · GitHub" />}
        {t === "terraform" && <Terraform />}
        {t === "ansible" && <Ansible />}
        {t === "git" && <Git />}
        {t === "files" && <Files />}
        {t === "cost" && <Cost />}
        {t === "backup" && <Backup />}
        {t === "forwards" && <Forwards />}
      </div>
    </div>
  );
}

const defaults: Record<string, number> = { ssh: 22, postgres: 5432, mysql: 3306, redis: 6379, mongodb: 27017 };

function Connections({ kinds, title }: { kinds: string[]; title: string }) {
  const s = useStore();
  const conns = useAsync(() => api.get("/connections"), []);
  const [edit, setEdit] = useState<any>(null);
  const [active, setActive] = useState<any>(null);
  const list = (conns.data || []).filter((c: any) => kinds.includes(c.kind));
  const groups: string[] = [...new Set<string>(list.map((c: any) => String(c.group || "")))];
  return (
    <>
      <h1 className="row"><span className="grow">{title}</span><button className="btn primary" onClick={() => setEdit({ kind: kinds[0], port: defaults[kinds[0]] })}><Plus size={13} />New connection</button></h1>
      {!list.length && <Empty>No saved connections.</Empty>}
      {groups.map((g) => <div key={g}>{g && <h2>{g}</h2>}
        <div className="grid g3">{list.filter((c: any) => (c.group || "") === g).map((c: any) => (
          <div className="card" key={c.id}>
            <div className="row"><b className="grow">{c.name}</b><Badge>{c.kind}</Badge></div>
            <div className="muted small">{c.user ? c.user + "@" : ""}{c.host || c.url}{c.port ? ":" + c.port : ""}{c.service ? ` · in-cluster svc ${c.namespace}/${c.service}` : ""}</div>
            <div className="row" style={{ marginTop: 8 }}>
              {["ssh", "postgres", "mysql", "redis", "mongodb"].includes(c.kind) && <button className="btn sm" onClick={() => s.openDock({ title: `${c.kind}: ${c.name}`, kind: "terminal", params: { cmd: `conn:${c.id}` } })}><SquareTerminal size={12} />Open</button>}
              {["postgres", "mysql", "redis", "mongodb"].includes(c.kind) && <button className="btn sm" onClick={() => setActive(c)}><Database size={12} />Query</button>}
              {["harbor", "vault", "jenkins", "gitlab", "github"].includes(c.kind) && <button className="btn sm" onClick={() => setActive(c)}><Plug size={12} />Browse</button>}
              <button className="btn sm" onClick={() => setEdit(c)}>Edit</button>
              <button className="btn sm danger" onClick={async () => { await api.del(`/connections/${c.id}`); conns.reload(); }}><Trash2 size={12} /></button>
            </div>
          </div>))}</div></div>)}
      {edit && <ConnEdit c={edit} kinds={kinds} onClose={() => { setEdit(null); conns.reload(); }} />}
      {active && (["harbor", "vault", "jenkins", "gitlab", "github"].includes(active.kind) ? <HttpBrowser c={active} onClose={() => setActive(null)} /> : <DbQuery c={active} onClose={() => setActive(null)} />)}
    </>
  );
}

function ConnEdit({ c, kinds, onClose }: { c: any; kinds: string[]; onClose: () => void }) {
  const s = useStore();
  const [f, setF] = useState({ ...c });
  const http = ["harbor", "vault", "jenkins", "gitlab", "github", "s3"].includes(f.kind);
  return (
    <Modal title="Connection" onClose={onClose} footer={<button className="btn primary" onClick={async () => { if (await act(() => api.post("/connections", f), "Saved")) onClose(); }}>Save</button>}>
      <div className="grid g3">
        <Field label="Type"><select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value, port: defaults[e.target.value] })}>{kinds.map((k) => <option key={k}>{k}</option>)}</select></Field>
        <Field label="Name"><input value={f.name || ""} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Group"><input value={f.group || ""} onChange={(e) => setF({ ...f, group: e.target.value })} /></Field>
        {http ? <Field label={f.kind === "s3" ? "Endpoint URL (empty = AWS)" : "URL"}><input value={f.url || ""} onChange={(e) => setF({ ...f, url: e.target.value })} /></Field>
          : <Field label="Host"><input value={f.host || ""} onChange={(e) => setF({ ...f, host: e.target.value })} /></Field>}
        {!http && <Field label="Port"><input type="number" value={f.port || ""} onChange={(e) => setF({ ...f, port: +e.target.value })} /></Field>}
        <Field label={f.kind === "s3" ? "Access key" : "User"}><input value={f.user || ""} onChange={(e) => setF({ ...f, user: e.target.value })} /></Field>
        <Field label={f.kind === "ssh" ? "Password (unused; use keys)" : f.kind === "s3" ? "Secret key" : "Password / token"}><input type="password" value={f.secret || ""} onChange={(e) => setF({ ...f, secret: e.target.value })} /></Field>
        {f.kind === "ssh" && <><Field label="Private key file"><input value={f.keyFile || ""} onChange={(e) => setF({ ...f, keyFile: e.target.value })} /></Field><Field label="Jump host"><input value={f.jump || ""} onChange={(e) => setF({ ...f, jump: e.target.value })} /></Field></>}
        {["postgres", "mysql", "mongodb"].includes(f.kind) && <Field label="Database"><input value={f.database || ""} onChange={(e) => setF({ ...f, database: e.target.value })} /></Field>}
        {f.kind === "s3" && <Field label="Region"><input value={f.region || ""} onChange={(e) => setF({ ...f, region: e.target.value })} /></Field>}
        {["postgres", "mysql", "redis", "mongodb"].includes(f.kind) && <>
          <Field label="In-cluster: cluster"><select value={f.clusterId || ""} onChange={(e) => setF({ ...f, clusterId: e.target.value })}><option value="">(direct connection)</option>{s.clusters.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</select></Field>
          <Field label="In-cluster: namespace"><input value={f.namespace || ""} onChange={(e) => setF({ ...f, namespace: e.target.value })} /></Field>
          <Field label="In-cluster: service"><input value={f.service || ""} onChange={(e) => setF({ ...f, service: e.target.value })} /></Field></>}
      </div>
    </Modal>
  );
}

function DbQuery({ c, onClose }: { c: any; onClose: () => void }) {
  const [q, setQ] = useState(c.kind === "redis" ? "INFO" : c.kind === "mongodb" ? "db.getCollectionNames()" : c.kind === "mysql" ? "SHOW TABLES;" : "SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema') LIMIT 100;");
  const [out, setOut] = useState("");
  const csv = c.kind === "postgres" || c.kind === "mysql";
  const rows = csv ? out.trim().split("\n").map((l) => l.split(c.kind === "mysql" ? "\t" : ",")) : [];
  return (
    <Modal wide title={`${c.kind}: ${c.name}`} onClose={onClose}>
      <textarea rows={5} value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="row" style={{ margin: "8px 0" }}><button className="btn primary" onClick={async () => { try { setOut((await api.post(`/connections/${c.id}/query`, { query: q })).output); } catch (e: any) { setOut("ERROR: " + e.message); } }}>Run</button>
        <span className="muted small">Executed with the {c.kind} CLI{c.service ? " through an automatic port-forward" : ""}.</span></div>
      <div className="grow" style={{ overflow: "auto" }}>
        {csv && rows.length > 1 && !out.startsWith("ERROR") ? <table className="tbl small"><thead><tr>{rows[0].map((h, i) => <th key={i}>{h}</th>)}</tr></thead><tbody>{rows.slice(1).map((r, i) => <tr key={i}>{r.map((v, j) => <td key={j}>{v}</td>)}</tr>)}</tbody></table> : <pre>{out}</pre>}
      </div>
    </Modal>
  );
}

function HttpBrowser({ c, onClose }: { c: any; onClose: () => void }) {
  const presets = useAsync(() => api.get("/integrations/presets"), []);
  const [ep, setEp] = useState("");
  const [params, setParams] = useState<Record<string, string>>({});
  const [res, setRes] = useState<any>(null);
  const [err, setErr] = useState("");
  const tpl = presets.data?.[c.kind]?.[ep] || ep;
  const vars = [...String(tpl).matchAll(/\{(\w+)\}/g)].map((m) => m[1]);
  return (
    <Modal wide title={`${c.kind}: ${c.name}`} onClose={onClose}>
      <div className="row wrap">
        {Object.keys(presets.data?.[c.kind] || {}).map((k) => <button key={k} className={`btn sm ${k === ep ? "primary" : ""}`} onClick={() => setEp(k)}>{k}</button>)}
        <input className="grow mono" placeholder="or a raw API path" value={ep} onChange={(e) => setEp(e.target.value)} />
      </div>
      <div className="row wrap" style={{ margin: "8px 0" }}>{vars.map((v) => <input key={v} placeholder={v} value={params[v] || ""} onChange={(e) => setParams({ ...params, [v]: e.target.value })} />)}
        <button className="btn primary" onClick={async () => { try { setErr(""); setRes(await api.post(`/connections/${c.id}/api`, { endpoint: ep, params })); } catch (e: any) { setErr(e.message); } }}>Call</button></div>
      <ErrorBox error={err} />
      <div className="grow" style={{ overflow: "auto" }}>{res && <ResultTable data={res} />}</div>
    </Modal>
  );
}

function ResultTable({ data }: { data: any }) {
  const arr = Array.isArray(data) ? data : Array.isArray(data?.jobs) ? data.jobs : Array.isArray(data?.workflow_runs) ? data.workflow_runs : Array.isArray(data?.builds) ? data.builds : Array.isArray(data?.data?.keys) ? data.data.keys.map((k: string) => ({ key: k })) : null;
  if (!arr || !arr.length || typeof arr[0] !== "object") return <JsonView data={data} />;
  const cols = Object.keys(arr[0]).filter((k) => typeof arr[0][k] !== "object").slice(0, 8);
  return <table className="tbl small"><thead><tr>{cols.map((k) => <th key={k}>{k}</th>)}</tr></thead><tbody>{arr.map((r: any, i: number) => <tr key={i}>{cols.map((k) => <td key={k}>{String(r[k] ?? "")}</td>)}</tr>)}</tbody></table>;
}

function S3() {
  const conns = useAsync(() => api.get("/connections?kind=s3"), []);
  const [cid, setCid] = useState("");
  const [bucket, setBucket] = useState("");
  const [prefix, setPrefix] = useState("");
  const [edit, setEdit] = useState<any>(null);
  const list = useAsync(() => (cid ? api.get(`/connections/${cid}/s3?bucket=${encodeURIComponent(bucket)}&prefix=${encodeURIComponent(prefix)}`) : Promise.resolve(null)), [cid, bucket, prefix]);
  return (
    <>
      <h1 className="row"><span className="grow">S3 browser</span><button className="btn" onClick={() => setEdit({ kind: "s3" })}><Plus size={13} />Add endpoint</button></h1>
      <div className="muted small">Works with AWS S3, MinIO, Ceph RGW, DigitalOcean Spaces, Wasabi, R2 and any S3-compatible store.</div>
      <div className="row" style={{ margin: "10px 0" }}>
        <select value={cid} onChange={(e) => { setCid(e.target.value); setBucket(""); setPrefix(""); }}><option value="">Select endpoint…</option>{(conns.data || []).map((c: any) => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        {bucket && <span className="mono">s3://{bucket}/{prefix}</span>}
        {bucket && <button className="btn sm" onClick={() => (prefix ? setPrefix(prefix.replace(/[^/]+\/$/, "")) : setBucket(""))}>Up</button>}
      </div>
      <ErrorBox error={list.error} />
      {!bucket && list.data?.Buckets && <table className="tbl"><tbody>{list.data.Buckets.map((b: any) => <tr key={b.Name} style={{ cursor: "pointer" }} onClick={() => setBucket(b.Name)}><td><Folder size={13} /> {b.Name}</td><td className="muted">{b.CreationDate}</td></tr>)}</tbody></table>}
      {bucket && list.data && <table className="tbl"><tbody>
        {(list.data.CommonPrefixes || []).map((p: any) => <tr key={p.Prefix} style={{ cursor: "pointer" }} onClick={() => setPrefix(p.Prefix)}><td><Folder size={13} /> {p.Prefix.slice(prefix.length)}</td><td /><td /></tr>)}
        {(list.data.Contents || []).filter((o: any) => o.Key !== prefix).map((o: any) => <tr key={o.Key}><td><FileCode size={13} /> {o.Key.slice(prefix.length)}</td><td>{bytes(o.Size)}</td><td className="muted">{o.LastModified}</td>
          <td><a className="btn sm" href={downloadUrl(`/connections/${cid}/s3/object`, { bucket, key: o.Key })}>Download</a></td></tr>)}
      </tbody></table>}
      {edit && <ConnEdit c={edit} kinds={["s3"]} onClose={() => { setEdit(null); conns.reload(); }} />}
    </>
  );
}

function Terraform() {
  const s = useStore();
  const [src, setSrc] = useState("");
  const [res, setRes] = useState<any>(null);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState<any>(null);
  const rs = (res?.resources || []).filter((r: any) => !q || r.address.includes(q));
  return (
    <>
      <h1>Terraform state viewer</h1>
      <div className="row"><input className="grow mono" placeholder="/path/terraform.tfstate · s3://bucket/key · k8s://<clusterId>/<ns>/tfstate-default-state" value={src} onChange={(e) => setSrc(e.target.value)} />
        <button className="btn" onClick={async () => { const p = await window.kubenova?.openDialog({ filters: [{ name: "tfstate", extensions: ["tfstate", "json"] }] }); if (p?.[0]) setSrc(p[0]); }}>Browse</button>
        <button className="btn primary" onClick={async () => setRes(await act(() => api.post("/terraform/state", { source: src })))}>Load</button></div>
      {res && <>
        <div className="row" style={{ margin: "10px 0" }}><Badge>{res.resources?.length} resources</Badge><Badge>terraform {res.meta?.terraformVersion}</Badge><Badge>serial {res.meta?.serial}</Badge>
          <input placeholder="filter address…" value={q} onChange={(e) => setQ(e.target.value)} />
          <button className="btn ai" onClick={() => s.openAI({ task: "chat", prompt: "Summarise this Terraform state: what infrastructure exists, and any risks.", context: rs.map((r: any) => `${r.address} id=${r.id}`).join("\n") })}>Explain</button></div>
        <div className="grid g2">
          <table className="tbl small"><thead><tr><th>Address</th><th>Provider</th><th>ID</th></tr></thead>
            <tbody>{rs.map((r: any) => <tr key={r.address} onClick={() => setSel(r)} style={{ cursor: "pointer" }}><td>{r.address}</td><td className="muted">{r.provider.replace(/provider\["registry.terraform.io\/|"\]/g, "")}</td><td className="mono">{r.id}</td></tr>)}</tbody></table>
          <div>{sel ? <JsonView data={sel.attributes} /> : res.meta?.outputs && <><h2>Outputs</h2><JsonView data={res.meta.outputs} /></>}</div>
        </div></>}
    </>
  );
}

function Ansible() {
  const [dir, setDir] = useState("");
  const [books, setBooks] = useState<string[]>([]);
  const [book, setBook] = useState("");
  const [inv, setInv] = useState("");
  const [check, setCheck] = useState(true);
  const [vars, setVars] = useState("");
  const [out, setOut] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <>
      <h1>Ansible</h1>
      <div className="row"><input className="grow mono" placeholder="playbook directory" value={dir} onChange={(e) => setDir(e.target.value)} />
        <button className="btn" onClick={async () => { const p = await window.kubenova?.openDialog({ directory: true }); if (p?.[0]) setDir(p[0]); }}>Browse</button>
        <button className="btn" onClick={async () => setBooks((await act(() => api.get(`/ansible/playbooks?dir=${encodeURIComponent(dir)}`))) || [])}>Scan</button></div>
      <div className="grid g2" style={{ marginTop: 10 }}>
        <div className="card"><h3>Playbooks</h3>{books.map((b) => <div key={b} className={`item`} style={{ cursor: "pointer", color: b === book ? "var(--accent)" : undefined }} onClick={() => setBook(b)}>{b.replace(dir, "")}</div>)}</div>
        <div className="col">
          <Field label="Inventory"><input value={inv} onChange={(e) => setInv(e.target.value)} placeholder="inventory.ini or host1,host2," /></Field>
          <Field label="Extra vars"><input value={vars} onChange={(e) => setVars(e.target.value)} placeholder="key=value or JSON" /></Field>
          <label className="inline"><input type="checkbox" checked={check} onChange={(e) => setCheck(e.target.checked)} />Check mode (dry run, --diff)</label>
          <button className="btn primary" disabled={!book || busy} onClick={async () => { setBusy(true); try { setOut((await api.post("/ansible/run", { playbook: book, inventory: inv, check, extraVars: vars })).output); } catch (e: any) { setOut(e.message); } setBusy(false); }}>{busy ? "Running…" : "Run playbook"}</button>
        </div>
      </div>
      {out && <pre style={{ maxHeight: "50vh" }}>{out}</pre>}
    </>
  );
}

function Git() {
  const s = useStore();
  const [dir, setDir] = useState(s.settings?.workdir || "");
  const [out, setOut] = useState("");
  const [msg, setMsg] = useState("");
  const run = async (...args: string[]) => { try { setOut((await api.post("/git", { dir, args })).output); } catch (e: any) { setOut(e.message); } };
  return (
    <>
      <h1>Git integration</h1>
      <div className="row"><input className="grow mono" placeholder="repository path" value={dir} onChange={(e) => setDir(e.target.value)} />
        <button className="btn" onClick={async () => { const p = await window.kubenova?.openDialog({ directory: true }); if (p?.[0]) setDir(p[0]); }}>Browse</button></div>
      <div className="row wrap" style={{ margin: "10px 0" }}>
        <button className="btn" onClick={() => run("status")}>Status</button><button className="btn" onClick={() => run("diff")}>Diff</button>
        <button className="btn" onClick={() => run("log", "--oneline", "-30", "--graph", "--decorate")}>Log</button><button className="btn" onClick={() => run("branch", "-a")}>Branches</button>
        <button className="btn" onClick={() => run("fetch", "--all")}>Fetch</button><button className="btn" onClick={() => run("pull", "--ff-only")}>Pull</button>
        <button className="btn" onClick={() => run("add", "-A")}>Stage all</button>
        <input placeholder="commit message" value={msg} onChange={(e) => setMsg(e.target.value)} /><button className="btn primary" disabled={!msg} onClick={() => run("commit", "-m", msg)}>Commit</button>
        <button className="btn" onClick={() => run("push")}>Push</button>
      </div>
      <pre style={{ maxHeight: "60vh" }}>{out.split("\n").map((l, i) => <div key={i} style={{ color: l.startsWith("+") ? "var(--ok)" : l.startsWith("-") ? "var(--err)" : undefined }}>{l || " "}</div>)}</pre>
    </>
  );
}

function Files() {
  const s = useStore();
  const [path, setPath] = useState("");
  const dir = useAsync(() => api.get(`/fs/list?path=${encodeURIComponent(path)}`), [path]);
  const [file, setFile] = useState<{ path: string; content: string } | null>(null);
  return (
    <>
      <h1>File explorer</h1>
      <div className="row"><input className="grow mono" value={dir.data?.path || path} onChange={(e) => setPath(e.target.value)} onKeyDown={(e) => e.key === "Enter" && setPath((e.target as HTMLInputElement).value)} />
        <button className="btn" onClick={() => setPath(dir.data?.parent || "")}>Up</button></div>
      <ErrorBox error={dir.error} />
      <div className="grid" style={{ gridTemplateColumns: "320px 1fr", marginTop: 10 }}>
        <div className="card" style={{ maxHeight: "70vh", overflow: "auto" }}>{(dir.data?.entries || []).map((e: any) => (
          <div key={e.path} className="row small" style={{ cursor: "pointer", padding: "3px 0" }} onClick={async () => (e.dir ? setPath(e.path) : setFile({ path: e.path, content: (await api.get(`/fs/read?path=${encodeURIComponent(e.path)}`)).content }))}>
            {e.dir ? <Folder size={13} /> : <FileCode size={13} />}<span className="grow">{e.name}</span>{!e.dir && <span className="muted">{bytes(e.size)}</span>}</div>))}</div>
        <div className="col">{file ? <>
          <div className="row"><b className="grow mono small">{file.path}</b>
            <button className="btn" onClick={async () => { const r = await api.post("/yaml/format", { yaml: file.content }); r.valid ? setFile({ ...file, content: r.formatted }) : s.notify((r.problems || [r.error]).join("; "), "error"); }}>Format & validate</button>
            {s.clusterId && <button className="btn" onClick={() => act(() => api.post(`/clusters/${s.clusterId}/apply`, { yaml: file.content, serverSide: true, force: true }), "Applied to cluster")}>Apply to cluster</button>}
            <button className="btn primary" onClick={() => act(() => api.put("/fs/write", file), "Saved")}>Save</button></div>
          <div style={{ height: "65vh" }}><YamlEditor value={file.content} onChange={(v) => setFile({ ...file, content: v })} /></div></> : <Empty>Select a file to edit.</Empty>}</div>
      </div>
    </>
  );
}

function Cost() {
  const s = useStore();
  const c = useAsync(() => (s.clusterId ? api.get(`/clusters/${s.clusterId}/cost`) : Promise.resolve(null)), [s.clusterId]);
  const [pricing, setPricing] = useState<any>(null);
  if (!s.clusterId) return <Empty>Select a cluster.</Empty>;
  const d = c.data;
  const money = (n: number) => `${d?.currency === "USD" ? "$" : ""}${n.toFixed(2)}`;
  const max = Math.max(...(d?.namespaces || []).map((n: any) => n.monthly), 1);
  return (
    <>
      <h1 className="row"><span className="grow">Cost dashboard</span><button className="btn" onClick={() => setPricing(d?.pricing)}>Pricing</button>
        <button className="btn ai" onClick={() => s.openAI({ task: "cost", prompt: "Suggest cost optimisations.", context: JSON.stringify({ total: d?.total, namespaces: d?.namespaces?.slice(0, 20), workloads: d?.workloads?.slice(0, 30), idle: d?.idle }) })}>AI cost optimization</button></h1>
      <ErrorBox error={c.error} />
      {d && <>
        <div className="grid g3"><div className="card"><h3>Estimated monthly cost</h3><div className="stat">{money(d.total)}</div><div className="muted small">from requests, PVCs and load balancers</div></div>
          <div className="card"><h3>Namespaces</h3><div className="stat">{d.namespaces?.length || 0}</div></div>
          <div className="card"><h3>Idle resources</h3><div className="stat" style={{ color: d.idle?.length ? "var(--warn)" : undefined }}>{d.idle?.length || 0}</div></div></div>
        <h2>By namespace</h2>
        {(d.namespaces || []).map((n: any) => <div key={n.namespace} className="row small" style={{ margin: "3px 0" }}><span style={{ width: 200 }}>{n.namespace}</span><div className="grow bar"><div style={{ width: `${(n.monthly / max) * 100}%` }} /></div><span style={{ width: 90, textAlign: "right" }}>{money(n.monthly)}</span></div>)}
        <h2>Top workloads</h2>
        <table className="tbl small"><thead><tr><th>Workload</th><th>Namespace</th><th>CPU req</th><th>Mem req</th><th>Monthly</th></tr></thead>
          <tbody>{(d.workloads || []).slice(0, 50).map((w: any) => <tr key={w.namespace + w.workload}><td>{w.workload}</td><td>{w.namespace}</td><td>{w.cpuCores.toFixed(2)}</td><td>{w.memGiB.toFixed(2)} GiB</td><td>{money(w.monthly)}</td></tr>)}</tbody></table>
        {d.idle?.length > 0 && <><h2>Idle resources</h2>{d.idle.map((i: string) => <div key={i} className="small">• {i}</div>)}</>}
      </>}
      {pricing && <Modal title="Pricing" onClose={() => setPricing(null)} footer={<button className="btn primary" onClick={async () => { await act(() => api.put("/cost/pricing", pricing), "Saved"); setPricing(null); c.reload(); }}>Save</button>}>
        <div className="grid g2">{["cpuHour", "memGiBHour", "diskGiBMonth", "lbMonth"].map((k) => <Field key={k} label={k}><input type="number" step="0.0001" value={pricing[k]} onChange={(e) => setPricing({ ...pricing, [k]: +e.target.value })} /></Field>)}
          <Field label="Currency"><input value={pricing.currency} onChange={(e) => setPricing({ ...pricing, currency: e.target.value })} /></Field></div>
      </Modal>}
    </>
  );
}

function Backup() {
  const s = useStore();
  const v = useAsync(() => (s.clusterId ? api.get(`/clusters/${s.clusterId}/velero`) : Promise.resolve(null)), [s.clusterId]);
  const [name, setName] = useState("");
  const [nss, setNss] = useState(s.namespace);
  if (!s.clusterId) return <Empty>Select a cluster.</Empty>;
  return (
    <>
      <h1>Backup & restore <span className="muted small">Velero</span></h1>
      <ErrorBox error={v.error} />
      <div className="row" style={{ marginBottom: 10 }}><input placeholder="backup name" value={name} onChange={(e) => setName(e.target.value)} /><input placeholder="namespaces (comma, empty = all)" value={nss} onChange={(e) => setNss(e.target.value)} />
        <button className="btn primary" disabled={!name} onClick={async () => { await act(() => api.post(`/clusters/${s.clusterId}/velero`, { kind: "backup", name, namespaces: nss.split(",").map((x) => x.trim()).filter(Boolean) }), "Backup created"); v.reload(); }}>Create backup</button></div>
      {["backups", "restores", "schedules", "backupstoragelocations"].map((k) => <div key={k}><h2>{k}</h2>
        <table className="tbl small"><thead><tr><th>Name</th><th>Phase</th><th>Created</th><th>Errors</th><th>Warnings</th><th></th></tr></thead>
          <tbody>{(v.data?.[k] || []).map((b: any) => <tr key={b.name}><td>{b.name}</td><td><Status s={b.phase} /></td><td>{String(b.created).slice(0, 19)}</td><td>{b.errors ?? ""}</td><td>{b.warnings ?? ""}</td>
            <td>{k === "backups" && b.phase === "Completed" && <button className="btn sm" onClick={async () => { await act(() => api.post(`/clusters/${s.clusterId}/velero`, { kind: "restore", name: `${b.name}-restore-${Date.now() % 100000}`, fromBackup: b.name }), "Restore started"); v.reload(); }}>Restore</button>}</td></tr>)}</tbody></table></div>)}
    </>
  );
}

function Forwards() {
  const f = useAsync(() => api.get("/portforwards"), []);
  return (
    <>
      <h1>Port forwards</h1>
      <table className="tbl"><thead><tr><th>Local</th><th>Target</th><th>Pod</th><th>Since</th><th></th></tr></thead>
        <tbody>{(f.data || []).map((x: any) => <tr key={x.id}><td><a href={`http://127.0.0.1:${x.localPort}`} target="_blank" rel="noreferrer">127.0.0.1:{x.localPort}</a></td><td>{x.kind}/{x.namespace}/{x.name}:{x.remotePort}</td><td>{x.pod}</td><td>{String(x.started).slice(11, 19)}</td>
          <td><button className="btn sm danger" onClick={async () => { await api.del(`/portforwards/${encodeURIComponent(x.id)}`); f.reload(); }}>Stop</button></td></tr>)}</tbody></table>
      {f.data && !f.data.length && <Empty>No active port forwards. Start one from a pod or service.</Empty>}
    </>
  );
}
