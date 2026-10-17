// Rancher single sign-on in the user's real browser (Chrome, Edge, Brave or
// Chromium). The browser runs with its own KubeNova profile and a local
// DevTools port; once the user has logged in to Rancher (any provider, and
// through Cloudflare or other bot checks), KubeNova reads Rancher's session
// cookie from that profile. Only the browser-level Storage domain is used, so
// pages are never attached to or scripted.
import { spawn, ChildProcess } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import WebSocket from "ws";

type Cookie = { name: string; value: string; domain: string };

function candidates(): string[] {
  const env = process.env.KUBENOVA_SSO_BROWSER;
  if (env) return [env];
  if (process.platform === "win32") {
    const dirs = [process.env["ProgramFiles"], process.env["ProgramFiles(x86)"], process.env.LOCALAPPDATA].filter(Boolean) as string[];
    const rel = ["Google\\Chrome\\Application\\chrome.exe", "Microsoft\\Edge\\Application\\msedge.exe", "BraveSoftware\\Brave-Browser\\Application\\brave.exe", "Chromium\\Application\\chrome.exe"];
    return rel.flatMap((r) => dirs.map((d) => path.join(d, r)));
  }
  if (process.platform === "darwin") {
    const apps = ["Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"];
    const exe: Record<string, string> = { "Brave Browser": "Brave Browser" };
    return apps.flatMap((a) => ["/Applications", path.join(process.env.HOME || "", "Applications")].map((d) => path.join(d, `${a}.app`, "Contents", "MacOS", exe[a] || a)));
  }
  const names = ["google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "chromium", "chromium-browser"];
  return names.flatMap((n) => (process.env.PATH || "").split(path.delimiter).map((d) => path.join(d, n)));
}

export function findBrowser(): string | null {
  return candidates().find((p) => { try { return fs.statSync(p).isFile(); } catch { return false; } }) || null;
}

// One browser per KubeNova profile; reused across logins.
let proc: ChildProcess | null = null;
let ws: WebSocket | null = null;
let seq = 0;

async function connect(profile: string): Promise<WebSocket> {
  if (ws && ws.readyState === WebSocket.OPEN) return ws;
  const file = path.join(profile, "DevToolsActivePort");
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    try {
      const [port, p] = fs.readFileSync(file, "utf8").split("\n");
      if (port && p) {
        const sock = new WebSocket(`ws://127.0.0.1:${port.trim()}${p.trim()}`, { perMessageDeflate: false });
        await new Promise<void>((res, rej) => { sock.once("open", () => res()); sock.once("error", rej); });
        sock.on("close", () => { if (ws === sock) ws = null; });
        ws = sock;
        return sock;
      }
    } catch { /* not ready yet */ }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error("The browser did not start its DevTools endpoint.");
}

function call<T>(sock: WebSocket, method: string, params: object = {}): Promise<T> {
  const id = ++seq;
  return new Promise((resolve, reject) => {
    const onMsg = (data: WebSocket.RawData) => {
      const m = JSON.parse(String(data));
      if (m.id !== id) return;
      sock.off("message", onMsg);
      m.error ? reject(new Error(m.error.message)) : resolve(m.result as T);
    };
    sock.on("message", onMsg);
    sock.send(JSON.stringify({ id, method, params }));
  });
}

const sessionCookie = (cookies: Cookie[], host: string) =>
  cookies.find((c) => c.name === "R_SESS" && c.value && (host === c.domain.replace(/^\./, "") || host.endsWith(c.domain.startsWith(".") ? c.domain : `.${c.domain}`)))?.value;

export type BrowserLoginResult = { token?: string; cancelled?: boolean; error?: string; noBrowser?: boolean };

// Opens Rancher's login page and resolves with the session token once the
// user is logged in. `isValid` lets the caller reject a stale session that is
// still in the profile from an earlier login.
export async function browserLogin(opts: { url: string; profile: string; isValid: (token: string) => Promise<boolean>; signal: { cancelled: boolean } }): Promise<BrowserLoginResult> {
  const exe = findBrowser();
  if (!exe) return { noBrowser: true };
  const base = new URL(opts.url);
  const loginUrl = `${base.origin}${base.pathname.replace(/\/$/, "")}/dashboard/auth/login`;
  fs.mkdirSync(opts.profile, { recursive: true });

  let sock: WebSocket;
  if (proc && proc.exitCode === null) {
    sock = await connect(opts.profile);
    await call(sock, "Target.createTarget", { url: loginUrl });
  } else {
    try { fs.rmSync(path.join(opts.profile, "DevToolsActivePort")); } catch { /* none */ }
    proc = spawn(exe, [`--user-data-dir=${opts.profile}`, "--remote-debugging-port=0", "--remote-allow-origins=http://127.0.0.1", "--no-first-run", "--no-default-browser-check", "--new-window", loginUrl], { stdio: "ignore", detached: false });
    proc.on("exit", () => { proc = null; ws?.close(); ws = null; });
    sock = await connect(opts.profile);
  }

  const tried = new Set<string>();
  while (!opts.signal.cancelled) {
    if (!proc) return { cancelled: true };
    let cookies: Cookie[] = [];
    try {
      cookies = (await call<{ cookies: Cookie[] }>(sock, "Storage.getCookies")).cookies;
    } catch {
      if (!proc) return { cancelled: true };
      try { sock = await connect(opts.profile); } catch { return { cancelled: true }; }
    }
    const token = sessionCookie(cookies, base.hostname);
    if (token && !tried.has(token)) {
      tried.add(token);
      const t = decodeURIComponent(token);
      if (await opts.isValid(t)) {
        closeLoginTabs(sock, base.origin).catch(() => {});
        return { token: t };
      }
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  return { cancelled: true };
}

// Closes the Rancher tabs we opened; if nothing else is open, the browser.
async function closeLoginTabs(sock: WebSocket, origin: string) {
  const { targetInfos } = await call<{ targetInfos: { targetId: string; type: string; url: string }[] }>(sock, "Target.getTargets");
  const pages = targetInfos.filter((t) => t.type === "page");
  const ours = pages.filter((t) => t.url.startsWith(origin));
  if (ours.length === pages.length) await call(sock, "Browser.close").catch(() => {});
  else for (const t of ours) await call(sock, "Target.closeTarget", { targetId: t.targetId }).catch(() => {});
}
