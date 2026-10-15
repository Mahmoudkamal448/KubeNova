import { useEffect, useRef } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { useStore } from "../store";

/** xterm.js bound to a backend PTY / exec WebSocket. */
export function Terminal({ url, active = true }: { url: string; active?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const fitRef = useRef<FitAddon>();
  const theme = useStore((s) => s.theme);
  useEffect(() => {
    if (!ref.current) return;
    const dark = theme !== "light";
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", Menlo, Consolas, monospace',
      theme: dark ? { background: "#0a0e15", foreground: "#e5e9f0", cursor: "#4f8cff" } : { background: "#f8fafc", foreground: "#172033", cursor: "#2563eb" },
      allowProposedApi: true,
      scrollback: 20000,
    });
    const fit = new FitAddon();
    fitRef.current = fit;
    term.loadAddon(fit);
    term.open(ref.current);
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    const sendSize = () => {
      try {
        fit.fit();
      } catch {
        /* hidden */
      }
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    };
    ws.onopen = () => {
      sendSize();
      term.focus();
    };
    ws.onmessage = (e) => term.write(typeof e.data === "string" ? e.data : new Uint8Array(e.data));
    ws.onclose = () => term.write("\r\n\x1b[90m[session closed]\x1b[0m\r\n");
    const d = term.onData((data) => ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify({ type: "input", data })));
    const ro = new ResizeObserver(() => sendSize());
    ro.observe(ref.current);
    return () => {
      d.dispose();
      ro.disconnect();
      ws.close();
      term.dispose();
    };
  }, [url, theme]);
  useEffect(() => {
    if (active) setTimeout(() => fitRef.current?.fit(), 50);
  }, [active]);
  return <div ref={ref} style={{ height: "100%", width: "100%", background: theme === "light" ? "#f8fafc" : "#0a0e15" }} />;
}
