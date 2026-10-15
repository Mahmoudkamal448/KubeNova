import { useEffect, useMemo, useState } from "react";
import { Pause, Play, RefreshCw, Sparkles } from "lucide-react";
import { api, qs } from "../api";
import { useStore } from "../store";
import { Badge, Empty, ErrorBox, useAsync } from "../components/ui";

/** Resource timeline with event replay (scrub or play back events over time). */
export function TimelineView() {
  const s = useStore();
  const [kind, setKind] = useState("");
  const [name, setName] = useState("");
  const [onlyWarn, setOnlyWarn] = useState(false);
  const ev = useAsync(() => (s.clusterId ? api.get(`/clusters/${s.clusterId}/timeline` + qs({ namespace: s.namespace, kind, name })) : Promise.resolve([])), [s.clusterId, s.namespace, kind, name]);
  const events = useMemo(() => (ev.data || []).filter((e: any) => !onlyWarn || e.type === "Warning"), [ev.data, onlyWarn]);
  const [cursor, setCursor] = useState(1);
  const [playing, setPlaying] = useState(false);
  useEffect(() => { setCursor(events.length); }, [events.length]);
  useEffect(() => {
    if (!playing) return;
    const id = setInterval(() => setCursor((c) => { if (c >= events.length) { setPlaying(false); return c; } return c + 1; }), 350);
    return () => clearInterval(id);
  }, [playing, events.length]);
  const shown = events.slice(0, cursor);
  const t0 = events.length ? new Date(events[0].time).getTime() : 0;
  const t1 = events.length ? new Date(events[events.length - 1].time).getTime() : 1;
  // State replay: latest reason per object at the cursor time.
  const state = useMemo(() => {
    const m = new Map<string, any>();
    for (const e of shown) m.set(`${e.kind}/${e.name}`, e);
    return [...m.values()].sort((a, b) => (a.type === "Warning" ? -1 : 1) - (b.type === "Warning" ? -1 : 1));
  }, [shown]);
  if (!s.clusterId) return <div className="page"><Empty>Select a cluster.</Empty></div>;
  return (
    <div className="page">
      <h1>Resource timeline & event replay</h1>
      <div className="row wrap" style={{ marginBottom: 10 }}>
        <input placeholder="Kind (e.g. Pod)" value={kind} onChange={(e) => setKind(e.target.value)} style={{ width: 140 }} />
        <input placeholder="Object name" value={name} onChange={(e) => setName(e.target.value)} style={{ width: 200 }} />
        <label className="inline"><input type="checkbox" checked={onlyWarn} onChange={(e) => setOnlyWarn(e.target.checked)} />Warnings only</label>
        <button className="btn" onClick={ev.reload}><RefreshCw size={13} /></button>
        <div className="grow" />
        <button className="btn ai" onClick={() => s.openAI({ task: "analyze-events", context: shown.slice(-300).map((e: any) => `${e.time} ${e.type} ${e.reason} ${e.kind}/${e.name}: ${e.message}`).join("\n") })}><Sparkles size={13} />Analyze</button>
      </div>
      <ErrorBox error={ev.error} />
      {events.length > 0 && <>
        <svg width="100%" height={44} viewBox="0 0 1000 44" preserveAspectRatio="none" style={{ background: "var(--bg2)", borderRadius: 8, border: "1px solid var(--border)" }}>
          {events.map((e: any, i: number) => {
            const x = ((new Date(e.time).getTime() - t0) / (t1 - t0 || 1)) * 990 + 5;
            return <line key={i} x1={x} x2={x} y1={e.type === "Warning" ? 6 : 20} y2={e.type === "Warning" ? 22 : 38} stroke={e.type === "Warning" ? "var(--warn)" : "var(--ok)"} opacity={i < cursor ? 1 : 0.25} />;
          })}
          {cursor > 0 && <line x1={((new Date(events[Math.min(cursor, events.length) - 1].time).getTime() - t0) / (t1 - t0 || 1)) * 990 + 5} x2={((new Date(events[Math.min(cursor, events.length) - 1].time).getTime() - t0) / (t1 - t0 || 1)) * 990 + 5} y1={0} y2={44} stroke="var(--accent)" strokeWidth={2} />}
        </svg>
        <div className="row" style={{ margin: "8px 0" }}>
          <button className="btn" onClick={() => { if (cursor >= events.length) setCursor(0); setPlaying(!playing); }}>{playing ? <Pause size={13} /> : <Play size={13} />}{playing ? "Pause" : "Replay"}</button>
          <input type="range" min={0} max={events.length} value={cursor} onChange={(e) => setCursor(+e.target.value)} className="grow" />
          <span className="small muted">{cursor}/{events.length} · {cursor ? new Date(events[cursor - 1].time).toLocaleString() : ""}</span>
        </div>
      </>}
      <div className="grid g2">
        <div className="card"><h3>Object state at cursor</h3>
          <table className="tbl small"><tbody>{state.slice(0, 200).map((e: any) => <tr key={e.kind + e.name}><td>{e.kind}/{e.name}</td><td><Badge kind={e.type === "Warning" ? "warn" : "ok"}>{e.reason}</Badge></td></tr>)}</tbody></table></div>
        <div className="card"><h3>Events</h3>
          <div style={{ maxHeight: "55vh", overflow: "auto" }}>{[...shown].reverse().slice(0, 500).map((e: any, i: number) => (
            <div key={i} className="small" style={{ borderBottom: "1px solid var(--border)", padding: "4px 0" }}>
              <span className="muted">{new Date(e.time).toLocaleTimeString()}</span> <Badge kind={e.type === "Warning" ? "warn" : "ok"}>{e.reason}</Badge> <b>{e.kind}/{e.name}</b> {e.count > 1 && <span className="muted">×{e.count}</span>}
              <div>{e.message}</div>
            </div>))}</div></div>
      </div>
      {!events.length && !ev.loading && <Empty>No events in range.</Empty>}
    </div>
  );
}
