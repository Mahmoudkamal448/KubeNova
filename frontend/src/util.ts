export function age(iso?: string): string {
  if (!iso) return "-";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return `${Math.floor(s)}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h${Math.floor((s % 3600) / 60) ? Math.floor((s % 3600) / 60) + "m" : ""}`;
  return `${Math.floor(s / 86400)}d`;
}

export function bytes(n: number): string {
  if (!n) return "0";
  const u = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

export function cores(milli: number): string {
  return milli >= 1000 ? `${(milli / 1000).toFixed(2)} cores` : `${milli}m`;
}

export function pct(a: number, b: number): number {
  return b ? Math.min(100, Math.round((a / b) * 100)) : 0;
}

export function download(name: string, content: string, type = "text/plain") {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([content], { type }));
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

export function copy(text: string) {
  navigator.clipboard?.writeText(text);
}

export function fmtUnit(v: number, unit?: string): string {
  switch (unit) {
    case "bytes":
      return bytes(v);
    case "percent":
      return `${(v * (v <= 1 ? 100 : 1)).toFixed(1)}%`;
    case "cores":
      return `${v.toFixed(3)}`;
    case "rps":
      return `${v.toFixed(1)}/s`;
  }
  return Math.abs(v) >= 1000 ? v.toExponential(2) : v.toFixed(2);
}
