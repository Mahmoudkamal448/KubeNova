import { useEffect, useMemo, useState } from "react";
import { Copy, Download, FileDiff, Folder, FileText, Play, RotateCcw, Scale, Sparkles, SquareTerminal, Trash2, Upload, X, Plug, Undo2, KeyRound } from "lucide-react";
import { api, downloadUrl, qs, rpath, wsUrl } from "../api";
import { useStore } from "../store";
import type { APIResource } from "../types";
import { act, Badge, Confirm, Empty, ErrorBox, Modal, Status, Tabs, useAsync, Field } from "../components/ui";
import { YamlEditor } from "../components/YamlEditor";
import { DiffView } from "../components/DiffView";
import { LogViewer } from "../components/LogViewer";
import { Terminal } from "../components/Terminal";
import { age, bytes, copy } from "../util";
import { PluginPanel } from "../plugins";

type Tab = string & {} | "overview" | "yaml" | "events" | "logs" | "shell" | "files" | "metrics" | "history" | "secret";

export function ResourceDetails({ res, name, namespace, onClose }: { res: APIResource; name: string; namespace: string; onClose: () => void }) {
  const s = useStore();
  const base = rpath(s.clusterId, res) + "/" + encodeURIComponent(name);
  const nsq = qs({ namespace });
  const obj = useAsync(() => api.get(base + nsq), [base, nsq]);
  const [tab, setTab] = useState<Tab>("overview");
  const [confirmDel, setConfirmDel] = useState(false);
  const [scaleOpen, setScaleOpen] = useState(false);
  const [cloneOpen, setCloneOpen] = useState(false);
  const [pfOpen, setPfOpen] = useState(false);
  const o = obj.data;
  const isPod = res.resource === "pods" && res.group === "";
  const isWorkload = ["deployments", "statefulsets", "daemonsets", "replicasets", "jobs"].includes(res.resource);
  const hasHistory = ["deployments", "statefulsets", "daemonsets"].includes(res.resource);
  const scalable = ["deployments", "statefulsets", "replicasets"].includes(res.resource) || !!o?.spec?.replicas;
  const containers: string[] = isPod ? [...(o?.spec?.initContainers || []), ...(o?.spec?.containers || [])].map((c: any) => c.name) : (o?.spec?.template?.spec?.containers || []).map((c: any) => c.name);
  const selector = o?.spec?.selector?.matchLabels ? Object.entries(o.spec.selector.matchLabels).map(([k, v]) => `${k}=${v}`).join(",") : "";

  const pluginTabs = useAsync(() => api.get("/plugins").then((ps: any[]) => ps.filter((p) => p.enabled).flatMap((p) => (p.panels || []).filter((x: any) => x.location === "resource-tab" && (!x.kinds?.length || x.kinds.includes(res.kind))))), [res.kind]);
  const tabs: { id: Tab; label: string }[] = [
    { id: "overview", label: "Overview" }, { id: "yaml", label: "YAML" }, { id: "events", label: "Events" },
    ...(isPod || (isWorkload && selector) ? [{ id: "logs" as Tab, label: "Logs" }] : []),
    ...(isPod ? [{ id: "shell" as Tab, label: "Shell" }, { id: "files" as Tab, label: "Files" }, { id: "metrics" as Tab, label: "Metrics" }] : []),
    ...(hasHistory ? [{ id: "history" as Tab, label: "History" }] : []),
    ...(res.resource === "secrets" ? [{ id: "secret" as Tab, label: "Decoded" }] : []),
    ...(pluginTabs.data || []).map((p: any) => ({ id: "plugin:" + p.id, label: p.title })),
  ];

  const troubleshoot = () => s.openAI({ task: isPod ? "troubleshoot-pod" : res.group.includes("fluxcd") ? "flux-failure" : res.group === "argoproj.io" ? "argocd-failure" : "chat",
    prompt: `Investigate ${res.kind} ${namespace ? namespace + "/" : ""}${name}.`, context: JSON.stringify({ kind: o?.kind, metadata: { name, namespace }, spec: o?.spec, status: o?.status }, null, 1).slice(0, 20000) });

  return (
    <div className="drawer">
      <div className="drawer-head">
        <span className="grow" style={{ minWidth: 0 }}>
          <div className="muted small">{res.kind}{namespace && ` · ${namespace}`}</div>
          <b style={{ fontSize: 15 }}>{name}</b>
        </span>
        <button className="btn ai sm" onClick={troubleshoot}><Sparkles size={13} />{isPod ? "Troubleshoot" : "Ask AI"}</button>
        {scalable && <button className="btn sm" onClick={() => setScaleOpen(true)}><Scale size={13} />Scale</button>}
        {isWorkload && res.resource !== "jobs" && <button className="btn sm" onClick={() => act(() => api.post(base + "/restart" + nsq), "Rollout restart triggered")}><RotateCcw size={13} />Restart</button>}
        {(isPod || res.resource === "services") && <button className="btn sm" onClick={() => setPfOpen(true)}><Plug size={13} />Forward</button>}
        <button className="btn sm" onClick={() => setCloneOpen(true)}><Copy size={13} />Clone</button>
        <a className="btn sm" href={downloadUrl(base.replace(/^\/clusters/, "/clusters") + "/export", { namespace })}><Download size={13} /></a>
        <button className="btn sm danger" onClick={() => setConfirmDel(true)}><Trash2 size={13} /></button>
        <button className="btn ghost sm" onClick={onClose}><X size={15} /></button>
      </div>
      <Tabs tabs={tabs} value={tab} onChange={setTab} />
      <div className="grow" style={{ overflow: "auto", minHeight: 0, padding: tab === "shell" || tab === "logs" ? 0 : 12 }}>
        <ErrorBox error={obj.error} />
        {o && tab === "overview" && <Overview o={o} res={res} />}
        {o && tab === "yaml" && <YamlTab base={base} nsq={nsq} reload={obj.reload} />}
        {tab === "events" && <EventsTab kind={res.kind} name={name} namespace={namespace} />}
        {tab === "logs" && <LogViewer clusterId={s.clusterId} namespace={namespace} pods={isPod ? [name] : undefined} labelSelector={isPod ? undefined : selector} containers={containers} />}
        {tab === "shell" && <ShellTab name={name} namespace={namespace} containers={containers} />}
        {tab === "files" && <FilesTab name={name} namespace={namespace} containers={containers} />}
        {tab === "metrics" && <PodMetrics name={name} namespace={namespace} />}
        {tab === "history" && <HistoryTab base={base} nsq={nsq} />}
        {tab === "secret" && <SecretTab name={name} namespace={namespace} />}
        {tab.startsWith("plugin:") && <PluginPanel id={tab.slice(7)} ctx={{ clusterId: s.clusterId, namespace, resource: { kind: res.kind, name, namespace, object: o } }} />}
        {isPod && tab === "overview" && <div className="row" style={{ marginTop: 12 }}>
          <button className="btn" onClick={() => s.openDock({ title: `logs: ${name}`, kind: "logs", params: { clusterId: s.clusterId, namespace, pods: name, containers: containers.join(",") } })}><FileText size={13} />Logs in dock</button>
          <button className="btn" onClick={() => s.openDock({ title: `sh: ${name}`, kind: "exec", params: { clusterId: s.clusterId, namespace, pod: name, container: containers[containers.length - 1] || "" } })}><SquareTerminal size={13} />Shell in dock</button>
        </div>}
      </div>
      {confirmDel && <Confirm text={<>Delete {res.kind} <b>{name}</b>?</>} onClose={() => setConfirmDel(false)} onYes={async () => { if (await act(() => api.del(base + nsq), "Deleted")) onClose(); }} />}
      {scaleOpen && <ScaleModal current={o?.spec?.replicas ?? 1} onClose={() => setScaleOpen(false)} onScale={(n) => act(() => api.post(base + "/scale" + nsq, { replicas: n }), `Scaled to ${n}`).then(obj.reload)} />}
      {cloneOpen && <CloneModal base={base} nsq={nsq} name={name} namespace={namespace} onClose={() => setCloneOpen(false)} />}
      {pfOpen && <PortForwardModal kind={isPod ? "pod" : "service"} name={name} namespace={namespace} obj={o} onClose={() => setPfOpen(false)} />}
    </div>
  );
}

function KV({ m }: { m?: Record<string, string> }) {
  if (!m || !Object.keys(m).length) return <span className="muted">none</span>;
  return <div className="row wrap">{Object.entries(m).map(([k, v]) => <span key={k} className="chip mono" title={`${k}=${v}`}>{k}={String(v).length > 60 ? String(v).slice(0, 60) + "…" : v}</span>)}</div>;
}

function Overview({ o, res }: { o: any; res: APIResource }) {
  const md = o.metadata || {};
  const podSpec = res.resource === "pods" ? o.spec : o.spec?.template?.spec;
  const statuses: Record<string, any> = {};
  for (const cs of [...(o.status?.initContainerStatuses || []), ...(o.status?.containerStatuses || [])]) statuses[cs.name] = cs;
  return (
    <div className="col" style={{ gap: 12 }}>
      <div className="grid g2">
        <div className="card"><h3>Metadata</h3>
          <table className="tbl small"><tbody>
            <tr><td className="muted">Created</td><td>{md.creationTimestamp} ({age(md.creationTimestamp)})</td></tr>
            <tr><td className="muted">UID</td><td className="mono">{md.uid}</td></tr>
            {md.ownerReferences?.map((r: any) => <tr key={r.uid}><td className="muted">Owner</td><td>{r.kind}/{r.name}</td></tr>)}
            {o.status?.phase && <tr><td className="muted">Phase</td><td><Status s={o.status.phase} /></td></tr>}
            {o.spec?.nodeName && <tr><td className="muted">Node</td><td>{o.spec.nodeName}</td></tr>}
            {o.status?.podIP && <tr><td className="muted">Pod IP</td><td>{o.status.podIP}</td></tr>}
            {o.status?.qosClass && <tr><td className="muted">QoS</td><td>{o.status.qosClass}</td></tr>}
            {o.spec?.serviceAccountName && <tr><td className="muted">Service account</td><td>{o.spec.serviceAccountName}</td></tr>}
          </tbody></table>
        </div>
        <div className="card"><h3>Labels</h3><KV m={md.labels} /><h3 style={{ marginTop: 10 }}>Annotations</h3><KV m={md.annotations} /></div>
      </div>
      {o.status?.conditions && <div className="card"><h3>Conditions</h3>
        <table className="tbl small"><thead><tr><th>Type</th><th>Status</th><th>Reason</th><th>Message</th><th>Last transition</th></tr></thead>
          <tbody>{o.status.conditions.map((c: any, i: number) => <tr key={i}><td>{c.type}</td><td><Status s={c.status === "True" ? "True" : c.status === "False" ? "False" : c.status} /></td><td>{c.reason}</td><td style={{ whiteSpace: "normal" }}>{c.message}</td><td>{age(c.lastTransitionTime)}</td></tr>)}</tbody></table>
      </div>}
      {podSpec && [...(podSpec.initContainers || []).map((c: any) => ({ ...c, init: true })), ...(podSpec.containers || [])].map((c: any) => {
        const st = statuses[c.name];
        const state = st ? Object.keys(st.state || {})[0] : "";
        return (
          <div className="card" key={c.name}>
            <h3>{c.init && <Badge>init</Badge>}{c.name} {st && <Status s={st.state?.waiting?.reason || st.state?.terminated?.reason || state} />}{st && <span className="muted small">restarts {st.restartCount}</span>}</h3>
            <div className="mono small">{c.image}</div>
            {st?.lastState?.terminated && <div className="small" style={{ color: "var(--warn)" }}>Last terminated: {st.lastState.terminated.reason} (exit {st.lastState.terminated.exitCode}) {age(st.lastState.terminated.finishedAt)} ago</div>}
            <div className="grid g2" style={{ marginTop: 8 }}>
              <div><div className="muted small">Resources</div><div className="mono small">req {JSON.stringify(c.resources?.requests || {})}<br />lim {JSON.stringify(c.resources?.limits || {})}</div></div>
              <div><div className="muted small">Ports</div><div className="mono small">{(c.ports || []).map((p: any) => `${p.containerPort}/${p.protocol || "TCP"}${p.name ? ` (${p.name})` : ""}`).join(", ") || "-"}</div></div>
            </div>
            {c.env?.length > 0 && <><div className="muted small" style={{ marginTop: 8 }}>Environment variables</div>
              <table className="tbl small"><tbody>{c.env.map((e: any) => <tr key={e.name}><td className="mono">{e.name}</td><td className="mono">{e.value ?? (e.valueFrom?.secretKeyRef ? `secret ${e.valueFrom.secretKeyRef.name}/${e.valueFrom.secretKeyRef.key}` : e.valueFrom?.configMapKeyRef ? `configmap ${e.valueFrom.configMapKeyRef.name}/${e.valueFrom.configMapKeyRef.key}` : JSON.stringify(e.valueFrom))}</td></tr>)}</tbody></table></>}
            {c.envFrom?.length > 0 && <div className="small mono">envFrom: {c.envFrom.map((e: any) => (e.secretRef ? `secret/${e.secretRef.name}` : `configmap/${e.configMapRef?.name}`)).join(", ")}</div>}
            {c.volumeMounts?.length > 0 && <><div className="muted small" style={{ marginTop: 8 }}>Mounted volumes</div>
              <table className="tbl small"><tbody>{c.volumeMounts.map((m: any) => {
                const v = (podSpec.volumes || []).find((x: any) => x.name === m.name) || {};
                const src = Object.keys(v).filter((k) => k !== "name")[0];
                return <tr key={m.mountPath}><td className="mono">{m.mountPath}</td><td>{m.name}</td><td className="muted">{src}{v[src]?.claimName ? `: ${v[src].claimName}` : v[src]?.name || v[src]?.secretName ? `: ${v[src].name || v[src].secretName}` : ""}</td><td>{m.readOnly ? "ro" : "rw"}</td></tr>;
              })}</tbody></table></>}
          </div>
        );
      })}
      {res.resource !== "pods" && o.spec && !podSpec && <div className="card"><h3>Spec</h3><pre style={{ maxHeight: 400 }}>{JSON.stringify(o.spec, null, 2)}</pre></div>}
      {o.status && res.resource !== "pods" && <div className="card"><h3>Status</h3><pre style={{ maxHeight: 300 }}>{JSON.stringify(o.status, null, 2)}</pre></div>}
    </div>
  );
}

function YamlTab({ base, nsq, reload }: { base: string; nsq: string; reload: () => void }) {
  const s = useStore();
  const [orig, setOrig] = useState("");
  const [text, setText] = useState("");
  const [diff, setDiff] = useState<any[] | null>(null);
  const [managed, setManaged] = useState(false);
  useEffect(() => {
    api.text(base + nsq + (nsq ? "&" : "?") + `format=yaml&managedFields=${managed}`).then((y) => { setOrig(y); setText(y); });
  }, [base, nsq, managed]);
  const apply = async (serverSide: boolean, dryRun = false) => {
    const r = await act(() => api.post(`/clusters/${s.clusterId}/apply`, { yaml: text, serverSide, force: serverSide, dryRun }));
    if (r) {
      const errs = r.filter((x: any) => x.error);
      if (errs.length) s.notify(errs.map((x: any) => x.error).join("; "), "error");
      else { s.notify(dryRun ? "Dry run OK" : "Applied", "ok"); if (!dryRun) reload(); }
    }
  };
  return (
    <div className="col" style={{ height: "100%" }}>
      <div className="row wrap">
        <button className="btn primary" onClick={() => apply(false)}><Play size={13} />Save</button>
        <button className="btn" onClick={() => apply(true)}>Server-side apply</button>
        <button className="btn" onClick={() => apply(true, true)}>Dry run</button>
        <button className="btn" onClick={async () => setDiff(await api.post(`/clusters/${s.clusterId}/diff`, { yaml: text }))}><FileDiff size={13} />Diff vs live</button>
        <button className="btn" onClick={async () => { const r = await api.post("/yaml/format", { yaml: text }); r.valid ? setText(r.formatted) : s.notify((r.problems || [r.error]).join("; "), "error"); }}>Format & validate</button>
        <button className="btn ai" onClick={() => s.openAI({ task: "explain-yaml", context: text })}><Sparkles size={13} />Explain</button>
        <button className="btn ghost" onClick={() => copy(text)}><Copy size={13} /></button>
        <label className="inline small"><input type="checkbox" checked={managed} onChange={(e) => setManaged(e.target.checked)} />managedFields</label>
        {text !== orig && <Badge kind="warn">modified</Badge>}
      </div>
      <div style={{ height: "calc(100vh - 230px)" }}><YamlEditor value={text} onChange={setText} /></div>
      {diff && <Modal title="Diff: live → desired (server-side dry run)" onClose={() => setDiff(null)} wide>
        {diff.map((d, i) => <div key={i} className="col grow" style={{ minHeight: 0 }}><b>{d.kind} {d.name}</b><DiffView a={d.live} b={d.desired} /></div>)}
      </Modal>}
    </div>
  );
}

function EventsTab({ kind, name, namespace }: { kind: string; name: string; namespace: string }) {
  const s = useStore();
  const ev = useAsync(() => api.get(`/clusters/${s.clusterId}/timeline` + qs({ namespace, kind, name })), [s.clusterId, kind, name, namespace]);
  if (ev.loading) return <div className="muted">Loading…</div>;
  if (!ev.data?.length) return <Empty>No events.</Empty>;
  return (
    <>
      <button className="btn ai sm" onClick={() => s.openAI({ task: "analyze-events", context: ev.data.map((e: any) => `${e.time} ${e.type} ${e.reason} ${e.kind}/${e.name}: ${e.message}`).join("\n") })}><Sparkles size={13} />Analyze events</button>
      <table className="tbl small" style={{ marginTop: 8 }}><thead><tr><th>Time</th><th>Type</th><th>Reason</th><th>Message</th><th>Count</th></tr></thead>
        <tbody>{[...ev.data].reverse().map((e: any, i: number) => <tr key={i}><td>{age(e.time)}</td><td><Status s={e.type === "Warning" ? "Warning" : "Normal"} /></td><td>{e.reason}</td><td style={{ whiteSpace: "normal" }}>{e.message}</td><td>{e.count}</td></tr>)}</tbody></table>
    </>
  );
}

function ShellTab({ name, namespace, containers }: { name: string; namespace: string; containers: string[] }) {
  const s = useStore();
  const [c, setC] = useState(containers[containers.length - 1] || "");
  return (
    <div className="col" style={{ height: "100%", gap: 0 }}>
      {containers.length > 1 && <div style={{ padding: 6 }}><select value={c} onChange={(e) => setC(e.target.value)}>{containers.map((x) => <option key={x}>{x}</option>)}</select></div>}
      <div className="grow" style={{ minHeight: 400 }}><Terminal key={c} url={wsUrl(`/clusters/${s.clusterId}/exec`, { namespace, pod: name, container: c })} /></div>
    </div>
  );
}

function FilesTab({ name, namespace, containers }: { name: string; namespace: string; containers: string[] }) {
  const s = useStore();
  const [c, setC] = useState(containers[containers.length - 1] || "");
  const [path, setPath] = useState("/");
  const files = useAsync(() => api.get(`/clusters/${s.clusterId}/files` + qs({ namespace, pod: name, container: c, path })), [path, c]);
  const upload = async (f: File) => {
    const fd = new FormData();
    fd.append("file", f);
    fd.append("namespace", namespace);
    fd.append("pod", name);
    fd.append("container", c);
    fd.append("path", path.endsWith("/") ? path : path + "/");
    await act(() => api.upload(`/clusters/${s.clusterId}/files/upload`, fd), `Uploaded ${f.name}`);
    files.reload();
  };
  const join = (p: string, n: string) => (p.endsWith("/") ? p : p + "/") + n;
  return (
    <div className="col">
      <div className="row">
        {containers.length > 1 && <select value={c} onChange={(e) => setC(e.target.value)}>{containers.map((x) => <option key={x}>{x}</option>)}</select>}
        <input className="grow mono" value={path} onChange={(e) => setPath(e.target.value)} />
        <button className="btn" onClick={() => setPath(path.replace(/\/[^/]+\/?$/, "") || "/")}>Up</button>
        <label className="btn"><Upload size={13} />Upload<input type="file" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} /></label>
      </div>
      <ErrorBox error={files.error} />
      <table className="tbl small"><thead><tr><th>Name</th><th>Size</th><th>Mode</th><th>Modified</th><th></th></tr></thead>
        <tbody>{(files.data || []).map((f: any) => (
          <tr key={f.name}>
            <td>{f.dir ? <a href="#" onClick={(e) => { e.preventDefault(); setPath(join(path, f.name)); }}><Folder size={12} /> {f.name}</a> : f.name}</td>
            <td>{f.dir ? "" : bytes(f.size)}</td><td className="mono">{f.mode}</td><td>{f.mtime}</td>
            <td>{!f.dir && <a className="btn sm" href={downloadUrl(`/clusters/${s.clusterId}/files/download`, { namespace, pod: name, container: c, path: join(path, f.name) })}><Download size={12} /></a>}</td>
          </tr>
        ))}</tbody></table>
    </div>
  );
}

function PodMetrics({ name, namespace }: { name: string; namespace: string }) {
  const s = useStore();
  const [hist, setHist] = useState<{ t: number; cpu: number; mem: number }[]>([]);
  const [err, setErr] = useState("");
  useEffect(() => {
    const tick = async () => {
      try {
        const all = await api.get(`/clusters/${s.clusterId}/metrics/pods` + qs({ namespace }));
        const m = all.find((x: any) => x.name === name);
        if (m) setHist((h) => [...h.slice(-59), { t: Date.now() / 1000, cpu: m.cpuMilli, mem: m.memoryBytes }]);
      } catch (e: any) {
        setErr(e.message);
      }
    };
    tick();
    const id = setInterval(tick, 10000);
    return () => clearInterval(id);
  }, [s.clusterId, name, namespace]);
  const last = hist[hist.length - 1];
  return (
    <div className="col">
      <ErrorBox error={err} />
      <div className="grid g2">
        <div className="card"><h3>CPU</h3><div className="stat">{last ? `${last.cpu}m` : "-"}</div><Spark values={hist.map((h) => h.cpu)} /></div>
        <div className="card"><h3>Memory</h3><div className="stat">{last ? bytes(last.mem) : "-"}</div><Spark values={hist.map((h) => h.mem)} /></div>
      </div>
      <div className="muted small">Live from metrics-server, sampled every 10s. Network/filesystem history is available in Monitoring when Prometheus is connected.</div>
    </div>
  );
}

export function Spark({ values }: { values: number[] }) {
  if (values.length < 2) return <div style={{ height: 40 }} />;
  const max = Math.max(...values) || 1;
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * 300},${40 - (v / max) * 36}`).join(" ");
  return <svg viewBox="0 0 300 40" style={{ width: "100%", height: 40 }}><polyline points={pts} fill="none" stroke="var(--accent)" strokeWidth={1.5} /></svg>;
}

function HistoryTab({ base, nsq }: { base: string; nsq: string }) {
  const h = useAsync(() => api.get(base + "/history" + nsq), [base]);
  return (
    <>
      <ErrorBox error={h.error} />
      <table className="tbl small"><thead><tr><th>Revision</th><th>Name</th><th>Images</th><th>Created</th><th>Cause</th><th></th></tr></thead>
        <tbody>{(h.data || []).map((r: any, i: number) => (
          <tr key={r.name}><td>{r.revision}{i === 0 && <Badge kind="ok">current</Badge>}</td><td>{r.name}</td><td className="mono">{(r.images || []).join(", ")}</td><td>{age(r.created)}</td><td>{r.cause}</td>
            <td>{i > 0 && <button className="btn sm" onClick={() => act(() => api.post(base + "/rollback" + nsq, { revision: r.revision }), `Rolled back to ${r.revision}`).then(h.reload)}><Undo2 size={12} />Rollback</button>}</td></tr>
        ))}</tbody></table>
    </>
  );
}

function SecretTab({ name, namespace }: { name: string; namespace: string }) {
  const s = useStore();
  const sec = useAsync(() => api.get(`/clusters/${s.clusterId}/secrets/${namespace}/${name}`), [name, namespace]);
  const [edit, setEdit] = useState<Record<string, string> | null>(null);
  const [reveal, setReveal] = useState<Record<string, boolean>>({});
  const d = sec.data;
  if (!d) return <ErrorBox error={sec.error} />;
  const data = edit || d.data;
  return (
    <div className="col">
      <div className="row"><Badge>{d.type}</Badge><div className="grow" />
        {edit ? <><button className="btn" onClick={() => setEdit(null)}>Cancel</button><button className="btn primary" onClick={async () => { await act(() => api.put(`/clusters/${s.clusterId}/secrets/${namespace}/${name}`, { data: edit, binary: d.binary }), "Secret updated"); setEdit(null); sec.reload(); }}>Save</button></>
          : <button className="btn" onClick={() => setEdit({ ...d.data })}><KeyRound size={13} />Edit</button>}
        {edit && <button className="btn" onClick={() => { const k = prompt("New key"); if (k) setEdit({ ...edit, [k]: "" }); }}>Add key</button>}
      </div>
      {d.certs?.map((c: any) => <div key={c.key} className="small"><Badge kind={c.daysLeft < 14 ? "error" : c.daysLeft < 30 ? "warn" : "ok"}>{c.key} expires in {c.daysLeft}d</Badge></div>)}
      {Object.entries(data).map(([k, v]) => (
        <div key={k} className="card">
          <div className="row"><b className="mono grow">{k}</b>{d.binary[k] && <Badge>binary (base64)</Badge>}
            <button className="btn sm ghost" onClick={() => setReveal({ ...reveal, [k]: !reveal[k] })}>{reveal[k] ? "Hide" : "Reveal"}</button>
            <button className="btn sm ghost" onClick={() => copy(String(v))}><Copy size={12} /></button>
            {edit && <button className="btn sm ghost danger" onClick={() => { const n = { ...edit }; delete n[k]; setEdit(n); }}><Trash2 size={12} /></button>}</div>
          {edit ? <textarea rows={3} style={{ width: "100%" }} value={String(v)} onChange={(e) => setEdit({ ...edit, [k]: e.target.value })} />
            : <pre style={{ margin: 0 }}>{reveal[k] ? String(v) : "••••••••"}</pre>}
        </div>
      ))}
    </div>
  );
}

function ScaleModal({ current, onClose, onScale }: { current: number; onClose: () => void; onScale: (n: number) => void }) {
  const [n, setN] = useState(current);
  return (
    <Modal title="Scale" onClose={onClose} footer={<button className="btn primary" onClick={() => { onScale(n); onClose(); }}>Scale to {n}</button>}>
      <div className="row"><input type="range" min={0} max={Math.max(20, current * 2)} value={n} onChange={(e) => setN(+e.target.value)} className="grow" /><input type="number" value={n} onChange={(e) => setN(+e.target.value)} style={{ width: 80 }} /></div>
    </Modal>
  );
}

function CloneModal({ base, nsq, name, namespace, onClose }: { base: string; nsq: string; name: string; namespace: string; onClose: () => void }) {
  const s = useStore();
  const [target, setTarget] = useState(s.clusterId);
  const [ns, setNs] = useState(namespace);
  const [newName, setNewName] = useState(name + "-copy");
  return (
    <Modal title="Clone resource" onClose={onClose} footer={<button className="btn primary" onClick={async () => { if (await act(() => api.post(base + "/clone" + nsq, { targetCluster: target, targetNamespace: ns, newName }), "Cloned")) onClose(); }}>Clone</button>}>
      <div className="grid g3">
        <Field label="Target cluster"><select value={target} onChange={(e) => setTarget(e.target.value)}>{s.clusters.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label="Namespace"><input value={ns} onChange={(e) => setNs(e.target.value)} /></Field>
        <Field label="New name"><input value={newName} onChange={(e) => setNewName(e.target.value)} /></Field>
      </div>
    </Modal>
  );
}

function PortForwardModal({ kind, name, namespace, obj, onClose }: { kind: string; name: string; namespace: string; obj: any; onClose: () => void }) {
  const s = useStore();
  const ports: number[] = useMemo(() => (kind === "pod" ? (obj?.spec?.containers || []).flatMap((c: any) => (c.ports || []).map((p: any) => p.containerPort)) : (obj?.spec?.ports || []).map((p: any) => p.port)), [obj, kind]);
  const [remote, setRemote] = useState(ports[0] || 80);
  const [local, setLocal] = useState(0);
  const [result, setResult] = useState<any>(null);
  return (
    <Modal title={`Port forward ${kind}/${name}`} onClose={onClose} footer={!result && <button className="btn primary" onClick={async () => setResult(await act(() => api.post(`/clusters/${s.clusterId}/portforwards`, { namespace, kind, name, localPort: local, remotePort: remote })))}>Start</button>}>
      {result ? <div>Forwarding <a href={`http://127.0.0.1:${result.localPort}`} target="_blank" rel="noreferrer">http://127.0.0.1:{result.localPort}</a> → {result.pod}:{result.remotePort}. Manage forwards in Toolbox.</div> : (
        <div className="grid g2">
          <Field label="Remote port"><input list="ports" type="number" value={remote} onChange={(e) => setRemote(+e.target.value)} /><datalist id="ports">{ports.map((p) => <option key={p} value={p} />)}</datalist></Field>
          <Field label="Local port (0 = random)"><input type="number" value={local} onChange={(e) => setLocal(+e.target.value)} /></Field>
        </div>
      )}
    </Modal>
  );
}
