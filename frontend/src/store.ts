import { create } from "zustand";
import { api } from "./api";
import type { APIResource, Cluster } from "./types";

export type View =
  | "clusters" | "explorer" | "helm" | "gitops" | "monitoring" | "security" | "topology" | "timeline"
  | "compare" | "search" | "rancher" | "cloud" | "toolbox" | "extensions" | "enterprise" | "settings";

export interface DockTab {
  id: string;
  title: string;
  kind: "terminal" | "logs" | "exec";
  params: Record<string, string>;
}

interface State {
  clusters: Cluster[];
  groups: string[];
  recent: Cluster[];
  clusterId: string;
  namespace: string;
  namespaces: string[];
  resources: APIResource[];
  view: View;
  selectedResource: APIResource | null;
  aiOpen: boolean;
  aiSeed: { task: string; context: string; prompt?: string } | null;
  dock: DockTab[];
  dockActive: string;
  dockOpen: boolean;
  theme: string;
  toast: { msg: string; kind: "info" | "error" | "ok" } | null;
  settings: any;
  loadClusters: (q?: string) => Promise<void>;
  selectCluster: (id: string) => Promise<void>;
  setNamespace: (ns: string) => void;
  setView: (v: View) => void;
  selectResource: (r: APIResource | null) => void;
  openAI: (seed?: { task: string; context: string; prompt?: string }) => void;
  closeAI: () => void;
  openDock: (t: Omit<DockTab, "id">) => void;
  closeDock: (id: string) => void;
  setDock: (patch: Partial<Pick<State, "dockOpen" | "dockActive">>) => void;
  notify: (msg: string, kind?: "info" | "error" | "ok") => void;
  setTheme: (t: string) => void;
  loadSettings: () => Promise<void>;
}

export const useStore = create<State>((set, get) => ({
  clusters: [],
  groups: [],
  recent: [],
  clusterId: localStorage.getItem("kubenova.cluster") || "",
  namespace: localStorage.getItem("kubenova.namespace") || "",
  namespaces: [],
  resources: [],
  view: "clusters",
  selectedResource: null,
  aiOpen: false,
  aiSeed: null,
  dock: [],
  dockActive: "",
  dockOpen: false,
  theme: localStorage.getItem("kubenova.theme") || "dark",
  toast: null,
  settings: null,
  async loadClusters(q = "") {
    const r = await api.get(`/clusters${q ? "?q=" + encodeURIComponent(q) : ""}`);
    set({ clusters: r.clusters || [], groups: r.groups || [], recent: r.recent || [] });
  },
  async selectCluster(id) {
    localStorage.setItem("kubenova.cluster", id);
    set({ clusterId: id, resources: [], namespaces: [], selectedResource: null });
    try {
      await api.post(`/clusters/${id}/connect`);
      const [resources, ns] = await Promise.all([
        api.get<APIResource[]>(`/clusters/${id}/api-resources`),
        api.get(`/clusters/${id}/r/core/v1/namespaces`).catch(() => ({ rows: [] })),
      ]);
      const pods = resources.find((r) => r.resource === "pods" && r.group === "");
      set({ resources, namespaces: (ns.rows || []).map((r: any) => r.name).sort(), selectedResource: get().selectedResource || pods || null });
      get().loadClusters();
    } catch (e: any) {
      get().notify(`Cannot connect: ${e.message}`, "error");
    }
  },
  setNamespace(ns) {
    localStorage.setItem("kubenova.namespace", ns);
    set({ namespace: ns });
  },
  setView: (v) => set({ view: v }),
  selectResource: (r) => set({ selectedResource: r, view: "explorer" }),
  openAI: (seed) => set({ aiOpen: true, aiSeed: seed ?? null }),
  closeAI: () => set({ aiOpen: false }),
  openDock(t) {
    const id = Math.random().toString(36).slice(2);
    set({ dock: [...get().dock, { ...t, id }], dockActive: id, dockOpen: true });
  },
  closeDock(id) {
    const dock = get().dock.filter((d) => d.id !== id);
    set({ dock, dockActive: dock.length ? dock[dock.length - 1].id : "", dockOpen: dock.length > 0 && get().dockOpen });
  },
  setDock: (patch) => set(patch),
  notify(msg, kind = "info") {
    set({ toast: { msg, kind } });
    setTimeout(() => get().toast?.msg === msg && set({ toast: null }), kind === "error" ? 7000 : 3500);
  },
  setTheme(t) {
    localStorage.setItem("kubenova.theme", t);
    set({ theme: t });
  },
  async loadSettings() {
    try {
      set({ settings: await api.get("/settings") });
    } catch {
      /* backend not ready */
    }
  },
}));

export const currentCluster = () => {
  const s = useStore.getState();
  return s.clusters.find((c) => c.id === s.clusterId);
};
