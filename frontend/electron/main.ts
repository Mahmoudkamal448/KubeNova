// Electron main process: launches the Go backend as a sidecar, passes its
// URL/token to the renderer and provides a few native integrations.
import { app, BrowserWindow, ipcMain, shell, dialog, Menu, safeStorage } from "electron";
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
