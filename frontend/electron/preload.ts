import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("kubenova", {
  backend: () => ipcRenderer.invoke("backend:info"),
  openExternal: (url: string) => ipcRenderer.invoke("shell:open", url),
  openDialog: (opts: unknown) => ipcRenderer.invoke("dialog:open", opts),
  encrypt: (s: string) => ipcRenderer.invoke("secure:encrypt", s),
  decrypt: (s: string) => ipcRenderer.invoke("secure:decrypt", s),
  rancherLogin: (opts: { url: string; insecure?: boolean }) => ipcRenderer.invoke("rancher:login", opts),
  rancherBrowserLogin: (opts: { url: string; serverId: string }) => ipcRenderer.invoke("rancher:browserLogin", opts),
  rancherBrowserLoginCancel: () => ipcRenderer.invoke("rancher:browserLoginCancel"),
  rancherClipboard: () => ipcRenderer.invoke("rancher:clipboard"),
  platform: process.platform,
});
