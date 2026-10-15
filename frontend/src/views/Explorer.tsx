import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight, Plus, RefreshCw, Sparkles, Wifi, WifiOff } from "lucide-react";
import { api, qs, rpath, wsUrl } from "../api";
import { useStore } from "../store";
import type { APIResource, Row } from "../types";
import { VirtualTable, type Column } from "../components/VirtualTable";
import { Badge, Empty, ErrorBox, Status } from "../components/ui";
import { age } from "../util";
import { ResourceDetails } from "./ResourceDetails";
import { CreateResource } from "./CreateResource";

const sectionOrder = ["Workloads", "Network", "Config", "Storage", "Access Control", "Cluster", "GitOps", "Monitoring", "Security", "Rancher", "Custom Resources"];
const favorites = ["pods", "deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "replicasets", "services", "ingresses", "endpoints", "endpointslices", "networkpolicies",
  "configmaps", "secrets", "persistentvolumeclaims", "persistentvolumes", "storageclasses", "namespaces", "nodes", "events", "serviceaccounts", "roles", "rolebindings",
  "clusterroles", "clusterrolebindings", "customresourcedefinitions", "resourcequotas", "limitranges", "priorityclasses", "leases", "runtimeclasses", "horizontalpodautoscalers",
  "poddisruptionbudgets", "gateways", "httproutes", "gatewayclasses", "grpcroutes"];

export function ExplorerView() {
  const s = useStore();
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster to explore its resources.</Empty></div>;
  return (
    <div className="split" style={{ flex: 1 }}>
      <ResourceSidebar />
      <div className="grow" style={{ display: "flex", flexDirection: "column", minWidth: 0, position: "relative" }}>
        {s.selectedResource ? <ResourceTable key={s.clusterId + rkey(s.selectedResource) + s.namespace} res={s.selectedResource} /> : <Empty>Pick a resource type.</Empty>}
      </div>
    </div>
  );
}

const rkey = (r: APIResource) => `${r.group}/${r.version}/${r.resource}`;

function ResourceSidebar() {
  const { resources, selectedResource, selectResource } = useStore();
  const [filter, setFilter] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({ "Custom Resources": false });
  const grouped = useMemo(() => {
    const f = filter.toLowerCase();
    const m = new Map<string, APIResource[]>();
    for (const r of resources) {
      if (f && !`${r.kind} ${r.resource} ${r.group} ${(r.shortNames || []).join(" ")}`.toLowerCase().includes(f)) continue;
      const sec = r.custom && r.section === "Custom Resources" ? `Custom Resources` : r.section;
      m.set(sec, [...(m.get(sec) || []), r]);
    }
    for (const [k, v] of m) {
      v.sort((a, b) => {
        const ia = favorites.indexOf(a.resource), ib = favorites.indexOf(b.resource);
        if (ia !== ib) return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib);
        return a.kind.localeCompare(b.kind);
      });
      m.set(k, v);
    }
    return [...m.entries()].sort((a, b) => (sectionOrder.indexOf(a[0]) + 100 * +(sectionOrder.indexOf(a[0]) < 0)) - (sectionOrder.indexOf(b[0]) + 100 * +(sectionOrder.indexOf(b[0]) < 0)));
  }, [resources, filter]);
  return (
    <div className="side">
      <input placeholder="Filter types (incl. CRDs)…" value={filter} onChange={(e) => setFilter(e.target.value)} style={{ width: "100%", marginBottom: 6 }} />
      {!resources.length && <div className="muted small" style={{ padding: 8 }}>Discovering API resources…</div>}
      {grouped.map(([sec, rs]) => (
        <div key={sec}>
          <div className="sec" onClick={() => setCollapsed({ ...collapsed, [sec]: !collapsed[sec] })}>
            <span className="row" style={{ gap: 4 }}>{collapsed[sec] ? <ChevronRight size={12} /> : <ChevronDown size={12} />}{sec}</span><span>{rs.length}</span>
          </div>
          {!collapsed[sec] && rs.map((r) => (
            <div key={rkey(r)} title={`${r.group || "core"}/${r.version}`} className={`item ${selectedResource && rkey(selectedResource) === rkey(r) ? "active" : ""}`} onClick={() => selectResource(r)}>
              <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{r.kind}</span>
              {r.custom && <span className="muted small" style={{ overflow: "hidden", textOverflow: "ellipsis", maxWidth: 90 }}>{r.group.split(".")[0]}</span>}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

const kindColumns: Record<string, { id: string; title: string; width?: string }[]> = {
  pods: [{ id: "ready", title: "Ready", width: "70px" }, { id: "restarts", title: "Restarts", width: "80px" }, { id: "node", title: "Node", width: "1fr" }, { id: "ip", title: "IP", width: "120px" }],
  deployments: [{ id: "ready", title: "Ready", width: "80px" }, { id: "upToDate", title: "Up-to-date", width: "90px" }, { id: "available", title: "Available", width: "80px" }, { id: "images", title: "Images", width: "2fr" }],
  statefulsets: [{ id: "ready", title: "Ready", width: "80px" }, { id: "images", title: "Images", width: "2fr" }],
  replicasets: [{ id: "ready", title: "Ready", width: "80px" }, { id: "images", title: "Images", width: "2fr" }],
  daemonsets: [{ id: "ready", title: "Ready", width: "80px" }, { id: "images", title: "Images", width: "2fr" }],
  jobs: [{ id: "completions", title: "Completions", width: "100px" }],
  cronjobs: [{ id: "schedule", title: "Schedule", width: "120px" }, { id: "active", title: "Active", width: "70px" }, { id: "lastSchedule", title: "Last schedule", width: "1fr" }],
  services: [{ id: "type", title: "Type", width: "110px" }, { id: "clusterIP", title: "Cluster IP", width: "120px" }, { id: "ports", title: "Ports", width: "1fr" }, { id: "external", title: "External", width: "1fr" }],
  ingresses: [{ id: "class", title: "Class", width: "100px" }, { id: "hosts", title: "Hosts", width: "2fr" }],
  nodes: [{ id: "roles", title: "Roles", width: "120px" }, { id: "version", title: "Version", width: "110px" }, { id: "cpu", title: "CPU", width: "60px" }, { id: "memory", title: "Memory", width: "110px" }, { id: "os", title: "OS", width: "1fr" }],
  persistentvolumeclaims: [{ id: "capacity", title: "Capacity", width: "90px" }, { id: "storageClass", title: "Storage class", width: "1fr" }, { id: "volume", title: "Volume", width: "1fr" }],
  persistentvolumes: [{ id: "capacity", title: "Capacity", width: "90px" }, { id: "storageClass", title: "Storage class", width: "1fr" }],
  events: [{ id: "reason", title: "Reason", width: "130px" }, { id: "object", title: "Object", width: "1fr" }, { id: "message", title: "Message", width: "3fr" }, { id: "count", title: "Count", width: "60px" }],
  secrets: [{ id: "type", title: "Type", width: "1fr" }, { id: "keys", title: "Keys", width: "60px" }],
  configmaps: [{ id: "keys", title: "Keys", width: "60px" }],
};

function ResourceTable({ res }: { res: APIResource }) {
  const s = useStore();
  const [rows, setRows] = useState<Row[]>([]);
  const [error, setError] = useState("");
  const [cached, setCached] = useState(false);
  const [live, setLive] = useState(false);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState("");
  const [labelSel, setLabelSel] = useState("");
  const [selected, setSelected] = useState<Row | null>(null);
  const [create, setCreate] = useState(false);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const map = useRef(new Map<string, Row>());
  const ns = res.namespaced ? s.namespace : "";
  const pageSize = s.settings?.pageSize || 500;

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    map.current.clear();
    let cont = "";
    let rv = "";
    try {
      // Lazy, paginated loading so very large clusters render progressively.
      do {
        const r = await api.get(rpath(s.clusterId, res) + qs({ namespace: ns, labelSelector: labelSel, limit: pageSize, continue: cont }));
        for (const row of r.rows as Row[]) map.current.set(row.uid, row);
        setRows([...map.current.values()]);
        setCached(!!r.cached);
        if (r.cached) setError(`Offline — showing cached data. ${r.error}`);
        cont = r.continue || "";
        rv = r.resourceVersion;
      } while (cont);
    } catch (e: any) {
      setError(e.message);
    }
    setLoading(false);
    return rv;
  }, [s.clusterId, res, ns, labelSel, pageSize]);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let closed = false;
    let pending: Row[] = [];
    let timer: number | undefined;
    const flush = () => {
      timer = undefined;
      setRows([...map.current.values()]);
      pending = [];
    };
    load().then((rv) => {
      if (closed || !res.verbs.includes("watch")) return;
      ws = new WebSocket(wsUrl(`/clusters/${s.clusterId}/watch`, { group: res.group || "core", version: res.version, resource: res.resource, namespace: ns, labelSelector: labelSel, resourceVersion: rv }));
      ws.onopen = () => setLive(true);
      ws.onclose = () => setLive(false);
      ws.onmessage = (e) => {
        const ev = JSON.parse(e.data);
        if (ev.type === "RELIST") { load(); return; }
        if (!ev.row) return;
        if (ev.type === "DELETED") map.current.delete(ev.row.uid);
        else map.current.set(ev.row.uid, ev.row);
        pending.push(ev.row);
        if (!timer) timer = window.setTimeout(flush, 250); // batch UI updates
      };
    });
    return () => {
      closed = true;
      ws?.close();
      if (timer) clearTimeout(timer);
    };
  }, [load, s.clusterId, res, ns, labelSel]);

  const shown = useMemo(() => {
    const f = filter.toLowerCase();
    return f ? rows.filter((r) => `${r.name} ${r.namespace} ${r.status} ${Object.values(r.cells || {}).join(" ")}`.toLowerCase().includes(f)) : rows;
  }, [rows, filter]);

  const columns: Column<Row>[] = [
    { id: "sel", title: "", width: "28px", render: (r) => <input type="checkbox" checked={checked.has(r.uid)} onClick={(e) => e.stopPropagation()} onChange={(e) => { const n = new Set(checked); e.target.checked ? n.add(r.uid) : n.delete(r.uid); setChecked(n); }} /> },
    { id: "name", title: "Name", width: "2fr", render: (r) => <span className="row"><span className={`dot ${r.health}`} />{r.name}</span>, sort: (r) => r.name },
    ...(res.namespaced ? [{ id: "ns", title: "Namespace", width: "1fr", render: (r: Row) => <span className="muted">{r.namespace}</span>, sort: (r: Row) => r.namespace || "" }] : []),
    ...(kindColumns[res.resource] || (res.custom ? [{ id: "message", title: "Message", width: "2fr" }] : [])).map((c) => ({ ...c, render: (r: Row) => r.cells?.[c.id] ?? "", sort: (r: Row) => (isNaN(+r.cells?.[c.id]) ? r.cells?.[c.id] || "" : +r.cells[c.id]) })),
    { id: "status", title: "Status", width: "130px", render: (r) => <Status s={r.status} />, sort: (r) => r.status },
    { id: "age", title: "Age", width: "70px", render: (r) => age(r.created), sort: (r) => -new Date(r.created).getTime() },
  ];

  const bulkDelete = async () => {
    if (!confirm(`Delete ${checked.size} ${res.kind}(s)?`)) return;
    for (const uid of checked) {
      const r = map.current.get(uid);
      if (r) await api.del(rpath(s.clusterId, res) + `/${r.name}` + qs({ namespace: r.namespace })).catch((e) => s.notify(e.message, "error"));
    }
    setChecked(new Set());
  };

  const troubleshoot = () => {
    const bad = rows.filter((r) => r.health === "error" || r.health === "warn").slice(0, 30);
    s.openAI({ task: res.resource === "pods" ? "troubleshoot-pod" : "chat", prompt: `These ${res.kind} objects are unhealthy in namespace ${ns || "(all)"}. Investigate the most important ones.`, context: bad.map((r) => `${r.namespace}/${r.name} status=${r.status} ${JSON.stringify(r.cells)}`).join("\n") });
  };

  return (
    <>
      <div className="row wrap" style={{ padding: "8px 12px", borderBottom: "1px solid var(--border)" }}>
        <b style={{ fontSize: 15 }}>{res.kind}</b>
        <span className="muted small">{res.group || "core"}/{res.version}</span>
        <Badge>{shown.length}{shown.length !== rows.length ? ` / ${rows.length}` : ""}</Badge>
        {live ? <Badge kind="ok"><Wifi size={11} />live</Badge> : <Badge kind={cached ? "warn" : ""}><WifiOff size={11} />{cached ? "cached" : "static"}</Badge>}
        {loading && <span className="muted small">loading…</span>}
        <div className="grow" />
        <input id="resource-filter" placeholder="Filter (/)…" value={filter} onChange={(e) => setFilter(e.target.value)} style={{ width: 200 }} />
        <input placeholder="Label selector, e.g. app=web" value={labelSel} onChange={(e) => setLabelSel(e.target.value)} onKeyDown={(e) => e.key === "Enter" && load()} style={{ width: 200 }} />
        {checked.size > 0 && <button className="btn danger" onClick={bulkDelete}>Delete {checked.size}</button>}
        {rows.some((r) => r.health === "error") && <button className="btn ai" onClick={troubleshoot}><Sparkles size={13} />Troubleshoot</button>}
        <button className="btn" onClick={() => load()}><RefreshCw size={13} /></button>
        <button className="btn primary" onClick={() => setCreate(true)}><Plus size={14} />Create</button>
      </div>
      <ErrorBox error={error} />
      <div className="grow" style={{ minHeight: 0 }}>
        {shown.length ? (
          <VirtualTable rows={shown} columns={columns} rowKey={(r) => r.uid} selected={selected?.uid} onRowClick={setSelected} />
        ) : !loading ? <Empty>No {res.kind} found{ns ? ` in ${ns}` : ""}.</Empty> : null}
      </div>
      {selected && <ResourceDetails res={res} name={selected.name} namespace={selected.namespace || ""} onClose={() => setSelected(null)} />}
      {create && <CreateResource res={res} onClose={() => setCreate(false)} />}
    </>
  );
}
