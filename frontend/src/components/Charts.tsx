import { fmtUnit } from "../util";

const palette = ["#4f8cff", "#2ec27e", "#f5a524", "#f2545b", "#7c5cff", "#38bdf8", "#e879f9", "#a3e635"];

export interface Series {
  name: string;
  points: [number, number][]; // [unix seconds, value]
}

/** Lightweight SVG time-series chart. */
export function LineChart({ series, height = 180, unit }: { series: Series[]; height?: number; unit?: string }) {
  const w = 600;
  const pts = series.flatMap((s) => s.points);
  if (!pts.length) return <div className="muted small" style={{ height, display: "grid", placeItems: "center" }}>No data</div>;
  const xs = pts.map((p) => p[0]), ys = pts.map((p) => p[1]);
  const x0 = Math.min(...xs), x1 = Math.max(...xs) || x0 + 1;
  const y1 = Math.max(...ys, 0) * 1.1 || 1;
  const sx = (x: number) => ((x - x0) / (x1 - x0 || 1)) * (w - 50) + 45;
  const sy = (y: number) => height - 20 - (y / y1) * (height - 30);
  return (
    <div>
      <svg viewBox={`0 0 ${w} ${height}`} style={{ width: "100%", height }}>
        {[0, 0.5, 1].map((t) => (
          <g key={t}>
            <line x1={45} x2={w} y1={sy(y1 * t)} y2={sy(y1 * t)} stroke="var(--border)" />
            <text x={2} y={sy(y1 * t) + 4} fontSize={10} fill="var(--fg3)">{fmtUnit(y1 * t, unit)}</text>
          </g>
        ))}
        {series.slice(0, 8).map((s, i) => (
          <polyline key={s.name} fill="none" stroke={palette[i % palette.length]} strokeWidth={1.6} points={s.points.map((p) => `${sx(p[0])},${sy(p[1])}`).join(" ")} />
        ))}
        <text x={45} y={height - 4} fontSize={10} fill="var(--fg3)">{new Date(x0 * 1000).toLocaleTimeString()}</text>
        <text x={w - 60} y={height - 4} fontSize={10} fill="var(--fg3)">{new Date(x1 * 1000).toLocaleTimeString()}</text>
      </svg>
      <div className="row wrap small">
        {series.slice(0, 8).map((s, i) => (
          <span key={s.name} className="row" style={{ gap: 4 }}>
            <span className="dot" style={{ background: palette[i % palette.length] }} />
            <span className="muted">{s.name}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

export function Gauge({ value, label, sub }: { value: number; label: string; sub?: string }) {
  const r = 38, c = 2 * Math.PI * r;
  const color = value > 90 ? "var(--err)" : value > 75 ? "var(--warn)" : "var(--ok)";
  return (
    <div className="col" style={{ alignItems: "center", gap: 2 }}>
      <svg width={96} height={96}>
        <circle cx={48} cy={48} r={r} stroke="var(--bg3)" strokeWidth={9} fill="none" />
        <circle cx={48} cy={48} r={r} stroke={color} strokeWidth={9} fill="none" strokeDasharray={`${(value / 100) * c} ${c}`} transform="rotate(-90 48 48)" strokeLinecap="round" />
        <text x={48} y={53} textAnchor="middle" fontSize={17} fontWeight={700} fill="var(--fg)">{value}%</text>
      </svg>
      <div style={{ fontWeight: 600 }}>{label}</div>
      {sub && <div className="muted small">{sub}</div>}
    </div>
  );
}

/** Convert a Prometheus API response to chart series. */
export function promToSeries(resp: any): Series[] {
  const res = resp?.data?.result || [];
  return res.map((r: any) => ({
    name: Object.entries(r.metric || {}).filter(([k]) => k !== "__name__").map(([k, v]) => `${k}=${v}`).join(",") || "value",
    points: (r.values || (r.value ? [r.value] : [])).map((p: [number, string]) => [Number(p[0]), parseFloat(p[1])] as [number, number]),
  }));
}
