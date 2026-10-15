import React, { useEffect, useState } from "react";
import { marked } from "marked";
import { X } from "lucide-react";
import { useStore } from "../store";

export function Modal(props: { title: React.ReactNode; onClose: () => void; children: React.ReactNode; footer?: React.ReactNode; wide?: boolean }) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => e.key === "Escape" && props.onClose();
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [props.onClose]);
  return (
    <div className="modal-bg" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
      <div className="modal" style={props.wide ? { width: "min(1300px, 96vw)", height: "88vh" } : undefined}>
        <div className="modal-head">
          <div className="grow">{props.title}</div>
          <button className="btn ghost sm" onClick={props.onClose}><X size={14} /></button>
        </div>
        <div className="modal-body" style={props.wide ? { flex: 1, display: "flex", flexDirection: "column", minHeight: 0 } : undefined}>{props.children}</div>
        {props.footer && <div className="modal-foot">{props.footer}</div>}
      </div>
    </div>
  );
}

export function Badge({ kind = "", children }: { kind?: string; children: React.ReactNode }) {
  return <span className={`badge ${kind}`}>{children}</span>;
}

export function healthKind(s?: string): string {
  if (!s) return "";
  const l = s.toLowerCase();
  if (["ok", "healthy", "ready", "running", "synced", "succeeded", "bound", "active", "complete", "deployed", "true"].some((x) => l === x || l.startsWith(x))) return "ok";
  if (["warn", "degraded", "pending", "progressing", "outofsync", "reconciling", "pending-upgrade", "pending-install", "containercreating", "suspended", "unknown", "terminating"].some((x) => l.includes(x))) return "warn";
  if (["error", "fail", "crash", "unreachable", "notready", "backoff", "missing", "expired", "critical", "oomkilled", "unavailable", "false"].some((x) => l.includes(x))) return "error";
  return "";
}

export function Status({ s }: { s?: string }) {
  if (!s) return <span className="muted">-</span>;
  return <Badge kind={healthKind(s)}>{s}</Badge>;
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: React.ReactNode }[]; value: T; onChange: (t: T) => void }) {
  return (
    <div className="tabs">
      {tabs.map((t) => (
        <button key={t.id} className={t.id === value ? "active" : ""} onClick={() => onChange(t.id)}>{t.label}</button>
      ))}
    </div>
  );
}

export function Bar({ value, kind }: { value: number; kind?: string }) {
  const k = kind ?? (value > 90 ? "error" : value > 75 ? "warn" : "ok");
  return <div className={`bar ${k}`}><div style={{ width: `${Math.min(100, value)}%` }} /></div>;
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function Markdown({ text, onApplyYaml }: { text: string; onApplyYaml?: (y: string) => void }) {
  const html = marked.parse(text || "", { async: false }) as string;
  const ref = React.useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current) return;
    ref.current.querySelectorAll("pre").forEach((pre) => {
      if (pre.querySelector(".copy-btn")) return;
      const code = pre.querySelector("code");
      const holder = document.createElement("div");
      holder.style.cssText = "position:absolute;top:4px;right:4px;display:flex;gap:4px";
      const copyBtn = document.createElement("button");
      copyBtn.className = "btn sm copy-btn";
      copyBtn.textContent = "Copy";
      copyBtn.onclick = () => navigator.clipboard?.writeText(code?.textContent || "");
      holder.appendChild(copyBtn);
      if (onApplyYaml && code?.className.includes("yaml")) {
        const apply = document.createElement("button");
        apply.className = "btn sm primary";
        apply.textContent = "Open in editor";
        apply.onclick = () => onApplyYaml(code.textContent || "");
        holder.appendChild(apply);
      }
      pre.appendChild(holder);
    });
  }, [html, onApplyYaml]);
  // Model output is rendered as markdown; strip raw HTML tags for safety.
  return <div ref={ref} className="markdown" dangerouslySetInnerHTML={{ __html: html.replace(/<(script|iframe|object|embed|style)[^>]*>[\s\S]*?<\/\1>/gi, "").replace(/ on\w+="[^"]*"/gi, "") }} />;
}

export function useAsync<T>(fn: () => Promise<T>, deps: React.DependencyList): { data: T | undefined; error: string; loading: boolean; reload: () => void } {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [n, setN] = useState(0);
  useEffect(() => {
    let alive = true;
    setLoading(true);
    setError("");
    fn()
      .then((d) => alive && setData(d))
      .catch((e) => alive && setError(e.message || String(e)))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, n]);
  return { data, error, loading, reload: () => setN((x) => x + 1) };
}

export function ErrorBox({ error }: { error: string }) {
  if (!error) return null;
  return <div className="card" style={{ borderColor: "var(--err)", color: "var(--err)", margin: "8px 0" }}>{error}</div>;
}

/** Runs an async action with toast feedback. */
export async function act<T>(fn: () => Promise<T>, success?: string): Promise<T | undefined> {
  const notify = useStore.getState().notify;
  try {
    const r = await fn();
    if (success) notify(success, "ok");
    return r;
  } catch (e: any) {
    notify(e.message || String(e), "error");
    return undefined;
  }
}

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label>{label}{children}</label>;
}

export function Confirm({ text, onYes, onClose, danger = true }: { text: React.ReactNode; onYes: () => void; onClose: () => void; danger?: boolean }) {
  return (
    <Modal title="Confirm" onClose={onClose} footer={<><button className="btn" onClick={onClose}>Cancel</button><button className={`btn ${danger ? "danger" : "primary"}`} onClick={() => { onYes(); onClose(); }}>Confirm</button></>}>
      {text}
    </Modal>
  );
}

export function JsonView({ data }: { data: unknown }) {
  return <pre style={{ maxHeight: 520 }}>{JSON.stringify(data, null, 2)}</pre>;
}
