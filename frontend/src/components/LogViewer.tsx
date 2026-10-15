import { useEffect, useMemo, useRef, useState } from "react";
import { Download, Pause, Play, Sparkles, Trash2 } from "lucide-react";
import { downloadUrl, wsUrl } from "../api";
import { useStore } from "../store";
import type { LogLine } from "../types";

/**
 * Live log viewer: multi-pod / multi-container streams, previous logs,
 * substring or regex search with highlighting, download and AI explain.
 */
export function LogViewer({ clusterId, namespace, pods, labelSelector, container, containers }: {
  clusterId: string; namespace: string; pods?: string[]; labelSelector?: string; container?: string; containers?: string[];
}) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [paused, setPaused] = useState(false);
  const [previous, setPrevious] = useState(false);
  const [ctr, setCtr] = useState(container || "");
  const [search, setSearch] = useState("");
  const [regex, setRegex] = useState(false);
  const [onlyMatches, setOnlyMatches] = useState(false);
  const [tail, setTail] = useState(500);
  const [timestamps, setTimestamps] = useState(false);
  const [err, setErr] = useState("");
  const buf = useRef<LogLine[]>([]);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const box = useRef<HTMLDivElement>(null);
  const openAI = useStore((s) => s.openAI);
  const params = { namespace, pods: pods?.join(","), labelSelector, container: ctr, previous, tail, follow: !previous, timestamps };

  useEffect(() => {
    setLines([]);
    buf.current = [];
    setErr("");
    const ws = new WebSocket(wsUrl(`/clusters/${clusterId}/logs`, params));
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.error) setErr(m.error);
      if (m.lines) {
        buf.current = buf.current.concat(m.lines);
        if (buf.current.length > 50000) buf.current = buf.current.slice(-50000);
        if (!pausedRef.current) setLines(buf.current);
      }
    };
    return () => ws.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId, namespace, pods?.join(","), labelSelector, ctr, previous, tail, timestamps]);

  useEffect(() => {
    if (!paused) {
      setLines(buf.current);
    }
  }, [paused]);

  useEffect(() => {
    if (!paused && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines, paused]);

  const matcher = useMemo(() => {
    if (!search) return null;
    try {
      return regex ? new RegExp(search, "gi") : new RegExp(search.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi");
    } catch {
      return null;
    }
  }, [search, regex]);

  const shown = useMemo(() => (matcher && onlyMatches ? lines.filter((l) => { matcher.lastIndex = 0; return matcher.test(l.line); }) : lines).slice(-5000), [lines, matcher, onlyMatches]);
  const multi = (pods?.length || 0) > 1 || !!labelSelector || !ctr;

  const highlight = (s: string) => {
    if (!matcher) return s;
    const parts: (string | JSX.Element)[] = [];
    let last = 0;
    matcher.lastIndex = 0;
    let m: RegExpExecArray | null;
    let i = 0;
    while ((m = matcher.exec(s)) && i++ < 50) {
      if (m[0] === "") break;
      parts.push(s.slice(last, m.index), <mark key={m.index}>{m[0]}</mark>);
      last = m.index + m[0].length;
    }
    parts.push(s.slice(last));
    return parts;
  };

  return (
    <div className="col" style={{ height: "100%", gap: 0 }}>
      <div className="row wrap" style={{ padding: 6, borderBottom: "1px solid var(--border)" }}>
        {containers && containers.length > 1 && (
          <select value={ctr} onChange={(e) => setCtr(e.target.value)}>
            <option value="">All containers</option>
            {containers.map((c) => <option key={c}>{c}</option>)}
          </select>
        )}
        <input placeholder={regex ? "Regex search…" : "Search logs…"} value={search} onChange={(e) => setSearch(e.target.value)} style={{ width: 240 }} />
        <label className="inline"><input type="checkbox" checked={regex} onChange={(e) => setRegex(e.target.checked)} />Regex</label>
        <label className="inline"><input type="checkbox" checked={onlyMatches} onChange={(e) => setOnlyMatches(e.target.checked)} />Only matches</label>
        <label className="inline"><input type="checkbox" checked={previous} onChange={(e) => setPrevious(e.target.checked)} />Previous</label>
        <label className="inline"><input type="checkbox" checked={timestamps} onChange={(e) => setTimestamps(e.target.checked)} />Timestamps</label>
        <select value={tail} onChange={(e) => setTail(+e.target.value)}>
          {[100, 500, 2000, 10000].map((n) => <option key={n} value={n}>tail {n}</option>)}
        </select>
        <div className="grow" />
        <span className="muted small">{lines.length} lines</span>
        <button className="btn sm" onClick={() => setPaused(!paused)}>{paused ? <Play size={13} /> : <Pause size={13} />}{paused ? "Resume" : "Pause"}</button>
        <button className="btn sm" onClick={() => { buf.current = []; setLines([]); }}><Trash2 size={13} /></button>
        <a className="btn sm" href={downloadUrl(`/clusters/${clusterId}/logs/download`, { ...params, tail: 0, follow: false })}><Download size={13} />Download</a>
        <button className="btn sm ai" onClick={() => openAI({ task: "explain-logs", context: lines.slice(-400).map((l) => `[${l.pod}/${l.container}] ${l.line}`).join("\n") })}><Sparkles size={13} />Explain</button>
      </div>
      {err && <div style={{ color: "var(--err)", padding: "4px 10px" }}>{err}</div>}
      <div ref={box} className="logview grow">
        {shown.map((l, i) => (
          <div className={`ln ${/error|exception|fatal|panic/i.test(l.line) ? "err" : ""}`} key={i}>
            {multi && <span className="src">{(pods?.length || 0) > 1 || labelSelector ? `${l.pod}/` : ""}{l.container}</span>}
            {highlight(l.line)}
          </div>
        ))}
      </div>
    </div>
  );
}
