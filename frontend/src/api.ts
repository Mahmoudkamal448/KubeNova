// Thin client for the KubeNova backend (HTTP + WebSocket).
declare global {
  interface Window {
    kubenova?: {
      backend: () => Promise<{ url: string; token: string; version: string; error?: string }>;
      openExternal: (url: string) => Promise<void>;
      openDialog: (opts: { directory?: boolean; filters?: { name: string; extensions: string[] }[] }) => Promise<string[]>;
      encrypt: (s: string) => Promise<string>;
      decrypt: (s: string) => Promise<string>;
      platform: string;
    };
  }
}

let base = "";
let token = "";

export async function initBackend(): Promise<string | null> {
  if (window.kubenova) {
    const info = await window.kubenova.backend();
    if (info.error) return info.error;
    base = info.url;
    token = info.token;
    return null;
  }
  // Browser / team-server mode: same origin, token from ?token= or localStorage.
  const params = new URLSearchParams(location.search);
  base = params.get("backend") || localStorage.getItem("kubenova.backend") || "http://127.0.0.1:7443";
  token = params.get("token") || localStorage.getItem("kubenova.token") || "";
  if (params.get("token")) localStorage.setItem("kubenova.token", token);
  if (params.get("backend")) localStorage.setItem("kubenova.backend", base);
  return null;
}

export class ApiError extends Error {
  status: number;
  constructor(msg: string, status: number) {
    super(msg);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown, raw = false): Promise<T> {
  const res = await fetch(base + "/api" + path, {
    method,
    headers: { "X-KubeNova-Token": token, ...(body !== undefined && !(body instanceof Blob) ? { "Content-Type": "application/json" } : {}) },
    body: body === undefined ? undefined : body instanceof Blob || body instanceof FormData ? (body as BodyInit) : JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      msg = j.error || msg;
    } catch {
      /* ignore */
    }
    throw new ApiError(msg, res.status);
  }
  if (raw) return (await res.text()) as unknown as T;
  const ct = res.headers.get("content-type") || "";
  return (ct.includes("json") ? res.json() : res.text()) as Promise<T>;
}

export const api = {
  get: <T = any>(p: string) => request<T>("GET", p),
  text: (p: string) => request<string>("GET", p, undefined, true),
  post: <T = any>(p: string, b?: unknown) => request<T>("POST", p, b ?? {}),
  put: <T = any>(p: string, b?: unknown) => request<T>("PUT", p, b ?? {}),
  patch: <T = any>(p: string, b?: unknown) => request<T>("PATCH", p, b ?? {}),
  del: <T = any>(p: string) => request<T>("DELETE", p),
  upload: <T = any>(p: string, form: FormData | Blob) => request<T>("POST", p, form),
};

export function wsUrl(path: string, params: Record<string, string | number | boolean | undefined> = {}) {
  const u = new URL(base.replace(/^http/, "ws") + "/api" + path);
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") u.searchParams.set(k, String(v));
  u.searchParams.set("token", token);
  return u.toString();
}

export function downloadUrl(path: string, params: Record<string, string | number | boolean | undefined> = {}) {
  const u = new URL(base + "/api" + path);
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") u.searchParams.set(k, String(v));
  u.searchParams.set("token", token);
  return u.toString();
}

export function openExternal(url: string) {
  if (window.kubenova) window.kubenova.openExternal(url);
  else window.open(url, "_blank");
}

export const qs = (o: Record<string, string | number | boolean | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== "") p.set(k, String(v));
  const s = p.toString();
  return s ? "?" + s : "";
};

/** Path segment for a resource type: /r/{group|core}/{version}/{resource} */
export const rpath = (cid: string, r: { group: string; version: string; resource: string }) =>
  `/clusters/${cid}/r/${r.group || "core"}/${r.version}/${r.resource}`;
