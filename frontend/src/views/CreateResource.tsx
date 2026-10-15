import { useState } from "react";
import { Sparkles } from "lucide-react";
import { api } from "../api";
import { useStore } from "../store";
import type { APIResource } from "../types";
import { act, Modal } from "../components/ui";
import { UnifiedDiff as UnifiedDiffInline } from "../components/DiffView";
import { YamlEditor } from "../components/YamlEditor";

const templates: Record<string, string> = {
  Deployment: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
  labels: { app: my-app }
spec:
  replicas: 2
  selector: { matchLabels: { app: my-app } }
  template:
    metadata: { labels: { app: my-app } }
    spec:
      securityContext: { runAsNonRoot: true, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: app
          image: nginxinc/nginx-unprivileged:1.27
          ports: [{ containerPort: 8080 }]
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { memory: 128Mi }
          readinessProbe: { httpGet: { path: /, port: 8080 } }
          securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: false, capabilities: { drop: [ALL] } }
`,
  Service: `apiVersion: v1
kind: Service
metadata: { name: my-app, namespace: default }
spec:
  selector: { app: my-app }
  ports: [{ port: 80, targetPort: 8080 }]
`,
  ConfigMap: `apiVersion: v1
kind: ConfigMap
metadata: { name: my-config, namespace: default }
data:
  key: value
`,
  Secret: `apiVersion: v1
kind: Secret
metadata: { name: my-secret, namespace: default }
type: Opaque
stringData:
  password: change-me
`,
  Ingress: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: { name: my-app, namespace: default }
spec:
  ingressClassName: nginx
  rules:
    - host: app.example.com
      http:
        paths:
          - { path: /, pathType: Prefix, backend: { service: { name: my-app, port: { number: 80 } } } }
`,
  CronJob: `apiVersion: batch/v1
kind: CronJob
metadata: { name: my-job, namespace: default }
spec:
  schedule: "*/15 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers: [{ name: job, image: busybox:1.36, command: ["sh", "-c", "date"] }]
`,
  NetworkPolicy: `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: default-deny, namespace: default }
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
`,
  HTTPRoute: `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: { name: my-app, namespace: default }
spec:
  parentRefs: [{ name: my-gateway }]
  hostnames: [app.example.com]
  rules: [{ backendRefs: [{ name: my-app, port: 80 }] }]
`,
  PersistentVolumeClaim: `apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: data, namespace: default }
spec:
  accessModes: [ReadWriteOnce]
  resources: { requests: { storage: 10Gi } }
`,
};

export function CreateResource({ res, onClose, initialYaml }: { res?: APIResource; onClose: () => void; initialYaml?: string }) {
  const s = useStore();
  const initial = (res && templates[res.kind]) || (res ? `apiVersion: ${res.group ? res.group + "/" : ""}${res.version}\nkind: ${res.kind}\nmetadata:\n  name: example\n${res.namespaced ? `  namespace: ${s.namespace || "default"}\n` : ""}spec: {}\n` : templates.Deployment);
  const [text, setText] = useState(initialYaml ?? initial.replace(/namespace: default/g, `namespace: ${s.namespace || "default"}`));
  const [results, setResults] = useState<any[] | null>(null);
  const [diff, setDiff] = useState<any[] | null>(null);
  const run = async (serverSide: boolean, dryRun: boolean) => {
    const r = await act(() => api.post(`/clusters/${s.clusterId}/apply`, { yaml: text, serverSide, force: serverSide, dryRun }));
    if (r) {
      setResults(r);
      if (!dryRun && !r.some((x: any) => x.error)) { s.notify("Applied", "ok"); onClose(); }
    }
  };
  return (
    <Modal wide title={`Create / apply ${res?.kind || "resources"}`} onClose={onClose} footer={<>
      <select onChange={(e) => e.target.value && setText(templates[e.target.value].replace(/namespace: default/g, `namespace: ${s.namespace || "default"}`))} defaultValue="">
        <option value="">Templates…</option>{Object.keys(templates).map((k) => <option key={k}>{k}</option>)}
      </select>
      <button className="btn ai" onClick={() => { s.openAI({ task: "generate-yaml", context: text, prompt: "" }); }}><Sparkles size={13} />Generate with AI</button>
      <div className="grow" />
      <button className="btn" onClick={async () => setDiff(await act(() => api.post(`/clusters/${s.clusterId}/diff`, { yaml: text })) || null)}>Diff</button>
      <button className="btn" onClick={() => run(true, true)}>Dry run</button>
      <button className="btn" onClick={() => run(false, false)}>Apply (client-side)</button>
      <button className="btn primary" onClick={() => run(true, false)}>Server-side apply</button>
    </>}>
      <div className="grow" style={{ minHeight: 0 }}><YamlEditor value={text} onChange={setText} /></div>
      {results && <div style={{ marginTop: 8 }}>{results.map((r, i) => <div key={i} className="small" style={{ color: r.error ? "var(--err)" : "var(--ok)" }}>{r.kind}/{r.name}: {r.error || r.action}</div>)}</div>}
      {diff && <div style={{ maxHeight: 260, overflow: "auto" }}>{diff.map((d, i) => <div key={i}><b>{d.kind} {d.name}</b>{d.new && " (new)"}<UnifiedDiffInline text={d.diff} /></div>)}</div>}
    </Modal>
  );
}
