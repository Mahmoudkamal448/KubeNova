import { Component, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Boxes, Building2, Cloud, Cpu, GitBranch, GitCompare, History, LayoutGrid, Network, Package, Puzzle, Search, Server,
  Settings as SettingsIcon, Shield, Sparkles, SquareTerminal, Wrench, X, Command, Star,
} from "lucide-react";
import { initBackend, wsUrl } from "./api";
import { useStore, type View } from "./store";
import { Terminal } from "./components/Terminal";
import { LogViewer } from "./components/LogViewer";
import { ClustersView } from "./views/Clusters";
import { ExplorerView } from "./views/Explorer";
import { HelmView } from "./views/Helm";
import { GitOpsView } from "./views/GitOps";
import { MonitoringView } from "./views/Monitoring";
import { SecurityView } from "./views/Security";
import { TopologyView } from "./views/Topology";
import { TimelineView } from "./views/Timeline";
import { CompareView } from "./views/Compare";
import { SearchView } from "./views/Search";
import { RancherView } from "./views/Rancher";
import { CloudView } from "./views/Cloud";
import { ToolboxView } from "./views/Toolbox";
import { ExtensionsView } from "./views/Extensions";
import { EnterpriseView } from "./views/Enterprise";
import { SettingsView } from "./views/Settings";
import { AIPanel } from "./views/AIPanel";
import { Badge } from "./components/ui";
import { usePluginThemes } from "./views/Extensions";
import { loadPlugins } from "./plugins";

const nav: { id: View; icon: JSX.Element; label: string }[] = [
  { id: "clusters", icon: <LayoutGrid size={19} />, label: "Clusters" },
  { id: "explorer", icon: <Boxes size={19} />, label: "Resources" },
  { id: "monitoring", icon: <Cpu size={19} />, label: "Monitoring" },
  { id: "helm", icon: <Package size={19} />, label: "Helm" },
  { id: "gitops", icon: <GitBranch size={19} />, label: "GitOps" },
  { id: "security", icon: <Shield size={19} />, label: "Security" },
  { id: "topology", icon: <Network size={19} />, label: "Topology" },
  { id: "timeline", icon: <History size={19} />, label: "Timeline & Replay" },
  { id: "compare", icon: <GitCompare size={19} />, label: "Compare clusters" },
  { id: "search", icon: <Search size={19} />, label: "Multi-cluster search" },
  { id: "rancher", icon: <Server size={19} />, label: "Rancher" },
  { id: "cloud", icon: <Cloud size={19} />, label: "Cloud providers" },
  { id: "toolbox", icon: <Wrench size={19} />, label: "Toolbox" },
  { id: "extensions", icon: <Puzzle size={19} />, label: "Extensions" },
  { id: "enterprise", icon: <Building2 size={19} />, label: "Teams & Enterprise" },
];

export function App() {
  const [ready, setReady] = useState<string | null | undefined>(undefined);
  const s = useStore();
  useEffect(() => {
    initBackend().then(async (err) => {
      setReady(err);
      if (!err) {
        await s.loadSettings();
        await s.loadClusters();
        loadPlugins();
        const id = useStore.getState().clusterId;
        if (id && useStore.getState().clusters.some((c) => c.id === id)) s.selectCluster(id);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    const t = s.theme === "system" ? (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark") : s.theme;
    document.documentElement.dataset.theme = t === "light" ? "light" : "dark";
    const custom = (window as any).__kubenovaThemes?.[s.theme];
    const root = document.documentElement.style;
    if (custom) for (const [k, v] of Object.entries(custom.colors || {})) root.setProperty(`--${k}`, String(v));
  }, [s.theme]);

  const [palette, setPalette] = useState<string | null>(null);
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      const target = e.target as HTMLElement;
      const typing = ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) || target.isContentEditable || target.closest(".xterm,.cm-editor");
      if (mod && e.key.toLowerCase() === "k") { e.preventDefault(); setPalette(""); }
      else if (mod && e.key.toLowerCase() === "i") { e.preventDefault(); s.aiOpen ? s.closeAI() : s.openAI(); }
      else if (mod && e.key === "`") { e.preventDefault(); s.openDock({ title: "Terminal", kind: "terminal", params: { clusterId: s.clusterId, namespace: s.namespace } }); }
      else if (mod && e.shiftKey && e.key.toLowerCase() === "f") { e.preventDefault(); s.setView("search"); }
      else if (mod && e.key >= "1" && e.key <= "9") { e.preventDefault(); const n = nav[+e.key - 1]; if (n) s.setView(n.id); }
      else if (!typing && e.key === ":") { e.preventDefault(); setPalette(":"); } // k9s-style command mode
      else if (!typing && e.key === "/" ) { e.preventDefault(); (document.querySelector("#resource-filter") as HTMLInputElement)?.focus(); }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [s]);

  if (ready === undefined) return <div className="empty" style={{ marginTop: "30vh" }}>Starting KubeNova…</div>;
  if (ready) return <div className="empty" style={{ marginTop: "25vh" }}><h2>Backend failed to start</h2><pre>{ready}</pre></div>;

  const cluster = s.clusters.find((c) => c.id === s.clusterId);
  return <Shell cluster={cluster} palette={palette} setPalette={setPalette} />;
}

function Shell({ cluster, palette, setPalette }: { cluster: ReturnType<typeof useStore.getState>["clusters"][number] | undefined; palette: string | null; setPalette: (p: string | null) => void }) {
  const s = useStore();
  usePluginThemes();
  const views: Record<View, JSX.Element> = {
    clusters: <ClustersView />, explorer: <ExplorerView />, helm: <HelmView />, gitops: <GitOpsView />, monitoring: <MonitoringView />,
    security: <SecurityView />, topology: <TopologyView />, timeline: <TimelineView />, compare: <CompareView />, search: <SearchView />,
    rancher: <RancherView />, cloud: <CloudView />, toolbox: <ToolboxView />, extensions: <ExtensionsView />, enterprise: <EnterpriseView />, settings: <SettingsView />,
  };

  return (
    <div className="app">
      <nav className="rail">
        <div className="logo">K</div>
        {nav.map((n) => (
          <button key={n.id} title={n.label} className={s.view === n.id ? "active" : ""} onClick={() => s.setView(n.id)}>{n.icon}</button>
        ))}
        <div className="spacer" />
        <button title="Terminal (Ctrl+`)" onClick={() => s.openDock({ title: "Terminal", kind: "terminal", params: { clusterId: s.clusterId, namespace: s.namespace } })}><SquareTerminal size={19} /></button>
        <button title="Settings" className={s.view === "settings" ? "active" : ""} onClick={() => s.setView("settings")}><SettingsIcon size={19} /></button>
      </nav>
      <header className="topbar">
        <select value={s.clusterId} onChange={(e) => s.selectCluster(e.target.value)} style={{ minWidth: 220, maxWidth: 320 }}>
          <option value="">Select cluster…</option>
          {s.recent.length > 0 && <optgroup label="Recent">{s.recent.map((c) => <option key={"r" + c.id} value={c.id}>{c.name}</option>)}</optgroup>}
          <optgroup label="All clusters">{s.clusters.map((c) => <option key={c.id} value={c.id}>{c.favorite ? "★ " : ""}{c.name}</option>)}</optgroup>
        </select>
        {cluster?.health && <span className="row small"><span className={`dot ${cluster.health.status}`} />{cluster.health.version} · {cluster.health.latencyMs}ms{cluster.health.cached && <Badge kind="warn">offline cache</Badge>}</span>}
        <select value={s.namespace} onChange={(e) => s.setNamespace(e.target.value)} style={{ minWidth: 160 }}>
          <option value="">All namespaces</option>
          {s.namespaces.map((n) => <option key={n}>{n}</option>)}
        </select>
        <div className="grow" />
        <button className="btn" onClick={() => setPalette("")} style={{ minWidth: 260, justifyContent: "space-between" }}>
          <span className="row muted"><Command size={13} />Search or run a command…</span><span className="kbd">Ctrl K</span>
        </button>
        <button className="btn ai" onClick={() => (s.aiOpen ? s.closeAI() : s.openAI())}><Sparkles size={14} />AI Assistant</button>
      </header>
      <main className="main">
        <div className="content"><ErrorBoundary key={s.view + s.clusterId}>{views[s.view]}</ErrorBoundary></div>
        <Dock />
        {s.aiOpen && <AIPanel />}
      </main>
      {palette !== null && <Palette initial={palette} onClose={() => setPalette(null)} />}
      {s.toast && <div className={`toast ${s.toast.kind}`}>{s.toast.msg}</div>}
    </div>
  );
}

function Dock() {
  const { dock, dockActive, dockOpen, setDock, closeDock } = useStore();
  const [height, setHeight] = useState(320);
  if (!dock.length) return null;
  return (
    <div className="dock" style={{ height: dockOpen ? height : 34 }}>
      <div className="row" style={{ borderBottom: "1px solid var(--border)", cursor: "ns-resize" }}
        onMouseDown={(e) => {
          const startY = e.clientY, startH = height;
          const mv = (ev: MouseEvent) => setHeight(Math.max(140, Math.min(window.innerHeight - 160, startH - (ev.clientY - startY))));
          const up = () => { window.removeEventListener("mousemove", mv); window.removeEventListener("mouseup", up); };
          window.addEventListener("mousemove", mv);
          window.addEventListener("mouseup", up);
        }}>
        <div className="tabs grow" style={{ borderBottom: 0 }}>
          {dock.map((d) => (
            <button key={d.id} className={d.id === dockActive ? "active" : ""} onMouseDown={(e) => e.stopPropagation()} onClick={() => setDock({ dockActive: d.id, dockOpen: true })}>
              {d.title}
              <X size={12} onClick={(e) => { e.stopPropagation(); closeDock(d.id); }} />
            </button>
          ))}
        </div>
        <button className="btn ghost sm" onMouseDown={(e) => e.stopPropagation()} onClick={() => setDock({ dockOpen: !dockOpen })}>{dockOpen ? "Minimize" : "Restore"}</button>
      </div>
      <div className="grow" style={{ position: "relative", minHeight: 0 }}>
        {dock.map((d) => (
          <div key={d.id} className={d.id === dockActive && dockOpen ? "" : "hidden"} style={{ position: "absolute", inset: 0 }}>
            {d.kind === "terminal" && <Terminal url={wsUrl("/terminal", d.params)} active={d.id === dockActive} />}
            {d.kind === "exec" && <Terminal url={wsUrl(`/clusters/${d.params.clusterId}/exec`, d.params)} active={d.id === dockActive} />}
            {d.kind === "logs" && (
              <LogViewer clusterId={d.params.clusterId} namespace={d.params.namespace} pods={d.params.pods?.split(",").filter(Boolean)} labelSelector={d.params.labelSelector}
                container={d.params.container} containers={d.params.containers?.split(",").filter(Boolean)} />
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

/** Command palette; ":" prefix gives a k9s-style resource jump (":po", ":deploy", ":crd"). */
function Palette({ initial, onClose }: { initial: string; onClose: () => void }) {
  const s = useStore();
  const [q, setQ] = useState(initial);
  const [sel, setSel] = useState(0);
  const items = useMemo(() => {
    const out: { label: string; hint?: string; icon?: JSX.Element; run: () => void }[] = [];
    const query = q.replace(/^:/, "").toLowerCase().trim();
    for (const r of s.resources) {
      const names = [r.resource, r.kind.toLowerCase(), ...(r.shortNames || [])];
      if (!query || names.some((n) => n.startsWith(query))) out.push({ label: `${r.kind}`, hint: `${r.group || "core"}/${r.version} · ${r.section}`, icon: <Boxes size={14} />, run: () => s.selectResource(r) });
    }
    if (!q.startsWith(":")) {
      for (const c of s.clusters) if (!query || c.name.toLowerCase().includes(query)) out.push({ label: `Switch to ${c.name}`, hint: c.server, icon: c.favorite ? <Star size={14} /> : <LayoutGrid size={14} />, run: () => s.selectCluster(c.id) });
      for (const n of nav) if (!query || n.label.toLowerCase().includes(query)) out.push({ label: `Go to ${n.label}`, icon: n.icon, run: () => s.setView(n.id) });
      const cmds: [string, () => void][] = [
        ["Open terminal", () => s.openDock({ title: "Terminal", kind: "terminal", params: { clusterId: s.clusterId, namespace: s.namespace } })],
        ["Open k9s", () => s.openDock({ title: "k9s", kind: "terminal", params: { clusterId: s.clusterId, namespace: s.namespace, cmd: "k9s" } })],
        ["Ask AI assistant", () => s.openAI()],
        ["Generate YAML with AI", () => s.openAI({ task: "generate-yaml", context: "", prompt: "" })],
        ["Toggle dark / light theme", () => s.setTheme(s.theme === "light" ? "dark" : "light")],
        ["Settings", () => s.setView("settings")],
      ];
      for (const [label, run] of cmds) if (!query || label.toLowerCase().includes(query)) out.push({ label, icon: <Command size={14} />, run });
      if (query) for (const n of s.namespaces) if (n.includes(query)) out.push({ label: `Namespace: ${n}`, icon: <Boxes size={14} />, run: () => s.setNamespace(n) });
    }
    return out.slice(0, 60);
  }, [q, s]);
  return (
    <div className="modal-bg" style={{ background: "rgba(0,0,0,.35)" }} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette">
        <input autoFocus value={q} placeholder="Type a command, resource (:po, :deploy), cluster or view…" onChange={(e) => { setQ(e.target.value); setSel(0); }}
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            if (e.key === "ArrowDown") { e.preventDefault(); setSel((x) => Math.min(items.length - 1, x + 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setSel((x) => Math.max(0, x - 1)); }
            if (e.key === "Enter" && items[sel]) { items[sel].run(); onClose(); }
          }} />
        <div style={{ maxHeight: 420, overflow: "auto" }}>
          {items.map((it, i) => (
            <div key={i} className={`opt ${i === sel ? "sel" : ""}`} onMouseEnter={() => setSel(i)} onClick={() => { it.run(); onClose(); }}>
              {it.icon}<span className="grow">{it.label}</span><span className="muted small">{it.hint}</span>
            </div>
          ))}
          {!items.length && <div className="empty">No matches</div>}
        </div>
      </div>
    </div>
  );
}

/** Keeps a failing view from taking down the whole window. */
class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="page">
        <div className="card" style={{ borderColor: "var(--err)" }}>
          <h3 style={{ color: "var(--err)" }}>This view failed to render</h3>
          <pre>{this.state.error.message}</pre>
          <button className="btn" onClick={() => this.setState({ error: null })}>Retry</button>
        </div>
      </div>
    );
  }
}
