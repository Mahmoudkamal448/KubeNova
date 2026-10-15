// Plugin host: loads enabled plugins' ES-module entries and lets them
// register panels rendered inside the IDE.
import { useEffect, useRef, useState } from "react";
import { api, downloadUrl } from "./api";
import { useStore } from "./store";

export interface PanelContext {
  clusterId: string;
  namespace: string;
  resource?: { kind: string; name: string; namespace: string; object: unknown };
  api: typeof api;
}
type Render = (el: HTMLElement, ctx: PanelContext) => void | (() => void);

const panels = new Map<string, Render>();
const listeners = new Set<() => void>();
let loaded = false;

export async function loadPlugins() {
  if (loaded) return;
  loaded = true;
  (window as any).kubenovaSDK = {
    version: 1,
    api,
    registerPanel(id: string, render: Render) {
      panels.set(id, render);
      listeners.forEach((l) => l());
    },
    get state() {
      const s = useStore.getState();
      return { clusterId: s.clusterId, namespace: s.namespace };
    },
  };
  const list = await api.get("/plugins").catch(() => []);
  for (const p of list) {
    if (!p.enabled || !p.entry) continue;
    try {
      await import(/* @vite-ignore */ downloadUrl(`/plugins/${p.id}/files/${p.entry}`));
    } catch (e) {
      console.error(`plugin ${p.id} failed to load`, e);
    }
  }
}

export function usePanels() {
  const [, setN] = useState(0);
  useEffect(() => {
    const l = () => setN((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  return panels;
}

export function PluginPanel({ id, ctx }: { id: string; ctx: Omit<PanelContext, "api"> }) {
  const ref = useRef<HTMLDivElement>(null);
  const all = usePanels();
  const render = all.get(id);
  useEffect(() => {
    if (!ref.current || !render) return;
    ref.current.innerHTML = "";
    const cleanup = render(ref.current, { ...ctx, api });
    return () => { if (typeof cleanup === "function") cleanup(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [render, ctx.clusterId, ctx.namespace, ctx.resource?.name]);
  if (!render) return <div className="muted">Panel “{id}” is not registered (plugin disabled or failed to load).</div>;
  return <div ref={ref} />;
}
