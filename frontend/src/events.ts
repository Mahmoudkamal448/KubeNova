// Live backend events (e.g. kubeconfig files added/changed/removed in a
// synced folder). Reconnects automatically.
import { wsUrl } from "./api";
import { useStore } from "./store";

let started = false;

export function watchEvents() {
  if (started) return;
  started = true;
  let timer: number | undefined;
  const connect = () => {
    const ws = new WebSocket(wsUrl("/events"));
    ws.onmessage = (e) => {
      const ev = JSON.parse(e.data);
      if (ev.type !== "clusters") return;
      clearTimeout(timer);
      // Debounce bursts (e.g. a git pull touching many files).
      timer = window.setTimeout(() => useStore.getState().loadClusters(), 300);
    };
    ws.onclose = () => setTimeout(connect, 3000);
  };
  connect();
}
