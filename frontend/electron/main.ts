// Electron main process: launches the Go backend as a sidecar, passes its
// URL/token to the renderer and provides a few native integrations.
import { app, BrowserWindow, ipcMain, shell, dialog, Menu, safeStorage, session, clipboard } from "electron";
import { spawn, ChildProcess } from "node:child_process";
import path from "node:path";
import fs from "node:fs";

let backend: ChildProcess | null = null;
let backendInfo: { url: string; token: string; version: string } | null = null;
let backendError = "";
const waiters: Array<(v: typeof backendInfo) => void> = [];

function backendPath(): string {
  const exe = process.platform === "win32" ? "kubenova-server.exe" : "kubenova-server";
  const candidates = [
    process.env.KUBENOVA_BACKEND || "",
    path.join(process.resourcesPath || "", "bin", exe),
    path.join(__dirname, "..", "resources", "bin", exe),
    path.join(__dirname, "..", "..", "backend", "bin", exe),
  ];
  return candidates.find((p) => p && fs.existsSync(p)) || candidates[1];
}

function startBackend() {
  const bin = backendPath();
  const args = ["--listen", "127.0.0.1:0"];
  if (process.env.KUBENOVA_DATA_DIR) args.push("--data-dir", process.env.KUBENOVA_DATA_DIR);
  backend = spawn(bin, args, {
    env: { ...process.env, KUBENOVA_TOOLS_DIR: path.join(process.resourcesPath || "", "bin") },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let buf = "";
  backend.stdout?.on("data", (d: Buffer) => {
    buf += d.toString();
    const m = buf.match(/KUBENOVA_READY (\{.*\})/);
    if (m && !backendInfo) {
      backendInfo = JSON.parse(m[1]);
      waiters.splice(0).forEach((w) => w(backendInfo));
    }
  });
  backend.stderr?.on("data", (d: Buffer) => {
    backendError = (backendError + d.toString()).slice(-4000);
  });
  backend.on("error", (e) => {
    backendError = `Failed to start backend at ${bin}: ${e.message}`;
    waiters.splice(0).forEach((w) => w(null));
  });
  backend.on("exit", (code) => {
    if (!backendInfo) {
      backendError ||= `backend exited with code ${code}`;
      waiters.splice(0).forEach((w) => w(null));
    }
  });
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1600,
    height: 1000,
    minWidth: 1100,
    minHeight: 700,
    backgroundColor: "#0b0f17",
    title: "KubeNova",
    titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "default",
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  win.webContents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: "deny" };
  });
  if (process.env.VITE_DEV_SERVER_URL) win.loadURL(process.env.VITE_DEV_SERVER_URL);
  else win.loadFile(path.join(__dirname, "..", "dist", "index.html"));
}

ipcMain.handle("backend:info", async () => {
  if (backendInfo) return backendInfo;
  return new Promise((resolve) => {
    waiters.push((v) => resolve(v ?? { error: backendError || "backend unavailable" }));
    setTimeout(() => resolve(backendInfo ?? { error: backendError || "timed out waiting for backend" }), 30000);
  });
});
ipcMain.handle("shell:open", (_e, url: string) => {
  if (/^https?:\/\//.test(url)) return shell.openExternal(url);
});
// Used while the user creates a Rancher API key in their browser. Only text
// shaped like a Rancher token, access key or secret key is passed on.
ipcMain.handle("rancher:clipboard", () => {
  const t = clipboard.readText().trim();
  return /^token-[a-z0-9]+(:[a-z0-9]+)?$/i.test(t) || /^[a-z0-9]{40,64}$/i.test(t) ? t : "";
});
ipcMain.handle("dialog:open", async (_e, opts: { directory?: boolean; filters?: { name: string; extensions: string[] }[] }) => {
  const r = await dialog.showOpenDialog({ properties: [opts?.directory ? "openDirectory" : "openFile", "multiSelections"], filters: opts?.filters });
  return r.canceled ? [] : r.filePaths;
});
ipcMain.handle("secure:encrypt", (_e, s: string) => (safeStorage.isEncryptionAvailable() ? safeStorage.encryptString(s).toString("base64") : s));
ipcMain.handle("secure:decrypt", (_e, s: string) => {
  try {
    return safeStorage.isEncryptionAvailable() ? safeStorage.decryptString(Buffer.from(s, "base64")) : s;
  } catch {
    return "";
  }
});

// Rancher login window: loads the server's own login page so every auth
// provider works (OIDC, Keycloak, Okta, Azure AD, GitHub, SAML, LDAP, local).
// When Rancher sets its session cookie (R_SESS, a regular Rancher API token)
// we hand that token to the backend and close the window. Each server gets a
// persistent partition, so the identity provider's own session is remembered.
ipcMain.handle("rancher:login", async (event, opts: { url: string; insecure?: boolean }) => {
  const base = new URL(opts.url);
  const prefix = base.pathname.replace(/\/$/, "");
  const partition = `persist:rancher-${base.host}`;
  const ses = session.fromPartition(partition);
  // Drop an old Rancher session so the login page is shown, but keep the
  // identity provider's cookies (single sign-on stays quick).
  for (const c of await ses.cookies.get({ url: base.origin, name: "R_SESS" })) {
    await ses.cookies.remove(base.origin + (c.path || "/"), c.name);
  }
  if (opts.insecure) {
    ses.setCertificateVerifyProc((req, cb) => cb(req.hostname === base.hostname ? 0 : -3));
  }
  // Some identity providers (e.g. Google) refuse embedded browsers that
  // announce themselves as Electron.
  ses.setUserAgent(ses.getUserAgent().replace(/\s?Electron\/[\d.]+/, "").replace(/\s?kubenova\/[\d.]+/i, ""));
  const parent = BrowserWindow.fromWebContents(event.sender) || undefined;
  const win = new BrowserWindow({
    width: 1000,
    height: 780,
    parent,
    title: `Log in to ${base.host}`,
    autoHideMenuBar: true,
    webPreferences: { partition, contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  return new Promise((resolve) => {
    let done = false;
    const finish = (result: { token?: string; cancelled?: boolean }) => {
      if (done) return;
      done = true;
      ses.cookies.removeListener("changed", onChange);
      clearInterval(timer);
      if (!win.isDestroyed()) win.close();
      resolve(result);
    };
    const onChange = (_e: unknown, cookie: Electron.Cookie, _cause: string, removed: boolean) => {
      if (!removed && cookie.name === "R_SESS" && cookie.value) finish({ token: decodeURIComponent(cookie.value) });
    };
    ses.cookies.on("changed", onChange);
    const timer = setInterval(async () => {
      const cs = await ses.cookies.get({ url: base.origin, name: "R_SESS" });
      const v = cs.find((c) => c.value)?.value;
      if (v) finish({ token: decodeURIComponent(v) });
    }, 1000);
    win.on("closed", () => finish({ cancelled: true }));
    // Identity providers sometimes open pop-ups; keep them in this window.
    win.webContents.setWindowOpenHandler(({ url }) => {
      win.loadURL(url);
      return { action: "deny" };
    });
    win.loadURL(`${base.origin}${prefix}/dashboard/auth/login`);
  });
});

app.whenReady().then(() => {
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(process.platform === "darwin" ? [{ role: "appMenu" as const }] : []),
      { role: "fileMenu" },
      { role: "editMenu" },
      { role: "viewMenu" },
      { role: "windowMenu" },
      { role: "help", submenu: [{ label: "Documentation", click: () => shell.openExternal("https://github.com/mahmoudkamal448/infrastructue-rebo") }] },
    ]),
  );
  startBackend();
  createWindow();
  app.on("activate", () => BrowserWindow.getAllWindows().length === 0 && createWindow());
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
app.on("before-quit", () => backend?.kill());
