import { useState } from "react";
import { AlertTriangle, FolderOpen, FolderSync, Plus, RefreshCw, Trash2, FileText } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import { act, Badge, Empty, Field, Modal, useAsync } from "../components/ui";
import { age } from "../util";

interface SyncDir { path: string; recursive: boolean; group?: string }
interface DirStatus extends SyncDir { exists: boolean; files: number; clusters: number; errors?: { path: string; error: string }[] }
interface FileStatus { path: string; dir?: string; origin: string; contexts: number; error?: string; modTime: string }

const groupModes = [
  { id: "@folder", label: "Folder name" },
  { id: "", label: "No group" },
  { id: "custom", label: "Custom name…" },
];

const originLabel: Record<string, string> = {
  "kubeconfig-env": "$KUBECONFIG", default: "~/.kube", synced: "synced folder", file: "added file", imported: "imported (Rancher / cloud / pasted)",
};

/** Manage synced kubeconfig folders and individual files. */
export function KubeconfigSources({ onClose }: { onClose: () => void }) {
  const s = useStore();
  const src = useAsync(() => api.get("/kubeconfigs"), []);
  const [edit, setEdit] = useState<SyncDir | null>(null);
  const [showFiles, setShowFiles] = useState(false);
  const status = src.data?.status;
  const dirs: DirStatus[] = status?.dirs || [];
  const files: FileStatus[] = status?.files || [];
  const others = files.filter((f) => f.origin !== "synced");
  const refresh = () => { src.reload(); s.loadClusters(); };

  const addFile = async () => {
    const paths = (await window.kubenova?.openDialog({})) || [prompt("Path to a kubeconfig file") || ""];
    for (const p of paths.filter(Boolean)) await act(() => api.post("/kubeconfigs", { path: p }), "Kubeconfig file added");
    refresh();
  };

  return (
    <Modal wide title={<span className="row"><FolderSync size={16} />Kubeconfig sources</span>} onClose={onClose}
      footer={<span className="muted small grow">Synced folders are rescanned every {Math.round((status?.intervalNs || 3e9) / 1e9)}s{status?.lastScan ? ` · last scan ${age(status.lastScan)} ago` : ""}. Change the interval in Settings.</span>}>
      <div className="col" style={{ overflow: "auto" }}>
        <div className="row">
          <div className="grow muted">KubeNova keeps these folders in sync: new kubeconfig files appear as clusters, edited files are reloaded and deleted files disappear. Files that aren't kubeconfigs (READMEs, Helm values…) are ignored.</div>
          <button className="btn" onClick={async () => { await act(() => api.post("/kubeconfigs/sync"), "Rescanned"); refresh(); }}><RefreshCw size={13} />Sync now</button>
          <button className="btn primary" onClick={() => setEdit({ path: "", recursive: true, group: "@folder" })}><Plus size={13} />Add folder</button>
        </div>

        <h2>Synced folders</h2>
        {!dirs.length && <Empty>No synced folders yet. <a href="#" onClick={(e) => { e.preventDefault(); setEdit({ path: "", recursive: true, group: "@folder" }); }}>Add one</a> — e.g. a shared drive or a git checkout with your team's kubeconfigs.</Empty>}
        {dirs.map((d) => (
          <div key={d.path} className="card">
            <div className="row">
              <FolderOpen size={16} />
              <b className="mono grow" style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{d.path}</b>
              {!d.exists && <Badge kind="error">folder missing</Badge>}
              <Badge kind={d.clusters ? "ok" : ""}>{d.clusters} clusters</Badge>
              <Badge>{d.files} files</Badge>
              <Badge>{d.recursive ? "incl. subfolders" : "top level only"}</Badge>
              <Badge>{d.group === "@folder" ? "group: folder name" : d.group ? `group: ${d.group}` : "no group"}</Badge>
              <button className="btn sm" onClick={() => setEdit({ path: d.path, recursive: d.recursive, group: d.group })}>Edit</button>
              <button className="btn sm danger" title="Stop syncing (files are not deleted)" onClick={async () => {
                if (!confirm(`Stop syncing ${d.path}? Its ${d.clusters} clusters will be removed from KubeNova; the files stay on disk.`)) return;
                await act(() => api.del(`/kubeconfigs/dirs?path=${encodeURIComponent(d.path)}`), "Folder no longer synced");
                refresh();
              }}><Trash2 size={12} /></button>
            </div>
            {(d.errors || []).map((e) => (
              <div key={e.path} className="small row" style={{ color: "var(--warn)", marginTop: 6 }}>
                <AlertTriangle size={12} /><span className="mono">{e.path.slice(d.path.length + 1)}</span>: {e.error}
              </div>
            ))}
          </div>
        ))}

        <h2 className="row" style={{ justifyContent: "space-between" }}>Other kubeconfig files
          <span className="row"><button className="btn sm" onClick={() => setShowFiles(!showFiles)}>{showFiles ? "Hide" : `Show ${others.length}`}</button><button className="btn sm" onClick={addFile}><Plus size={12} />Add file</button></span></h2>
        {showFiles && (
          <table className="tbl small"><thead><tr><th>File</th><th>From</th><th>Contexts</th><th>Modified</th><th></th></tr></thead>
            <tbody>{others.map((f) => (
              <tr key={f.path}>
                <td className="mono"><FileText size={11} /> {f.path}{f.error && <div style={{ color: "var(--warn)" }}>{f.error}</div>}</td>
                <td>{originLabel[f.origin] || f.origin}</td><td>{f.contexts}</td><td>{f.modTime && !f.modTime.startsWith("0001") ? age(f.modTime) + " ago" : "-"}</td>
                <td>{(f.origin === "file" || f.origin === "imported") && <button className="btn sm danger" onClick={async () => { await act(() => api.del(`/kubeconfigs?path=${encodeURIComponent(f.path)}`), "Removed"); refresh(); }}><Trash2 size={12} /></button>}</td>
              </tr>
            ))}</tbody></table>
        )}
      </div>
      {edit && <EditDir initial={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); refresh(); }} />}
    </Modal>
  );
}

function EditDir({ initial, onClose, onSaved }: { initial: SyncDir; onClose: () => void; onSaved: () => void }) {
  const isNew = !initial.path;
  const [d, setD] = useState<SyncDir>(initial);
  const mode = d.group === "@folder" ? "@folder" : d.group ? "custom" : "";
  const browse = async () => {
    const p = await window.kubenova?.openDialog({ directory: true });
    if (p?.[0]) setD({ ...d, path: p[0] });
  };
  return (
    <Modal title={isNew ? "Add synced folder" : "Edit synced folder"} onClose={onClose}
      footer={<button className="btn primary" disabled={!d.path} onClick={async () => { if (await act(() => api.post("/kubeconfigs/dirs", d), isNew ? "Folder synced" : "Saved")) onSaved(); }}>{isNew ? "Start syncing" : "Save"}</button>}>
      <div className="col">
        <Field label="Folder">
          <div className="row">
            <input className="grow mono" value={d.path} readOnly={!isNew} placeholder={navigator.platform.startsWith("Win") ? "C:\\Users\\me\\kubeconfigs" : "/home/me/kubeconfigs"} onChange={(e) => setD({ ...d, path: e.target.value })} />
            {isNew && window.kubenova && <button className="btn" onClick={browse}><FolderOpen size={13} />Browse</button>}
          </div>
        </Field>
        <label className="inline"><input type="checkbox" checked={d.recursive} onChange={(e) => setD({ ...d, recursive: e.target.checked })} />Include subfolders (hidden folders like <span className="mono">.git</span> are skipped)</label>
        <Field label="Group clusters by">
          <div className="row">
            <select value={mode} onChange={(e) => setD({ ...d, group: e.target.value === "custom" ? "My clusters" : e.target.value })}>
              {groupModes.map((g) => <option key={g.id} value={g.id}>{g.label}</option>)}
            </select>
            {mode === "custom" && <input className="grow" value={d.group} onChange={(e) => setD({ ...d, group: e.target.value })} />}
          </div>
        </Field>
        <div className="muted small">
          {mode === "@folder" ? <>Each cluster goes into a group named after the folder its file is in, e.g. <span className="mono">prod/eu.yaml</span> → group <b>prod</b>.</> : mode === "custom" ? "All clusters from this folder go into one group." : "Clusters are not grouped."} A group you set on a cluster yourself always wins.
        </div>
      </div>
    </Modal>
  );
}
