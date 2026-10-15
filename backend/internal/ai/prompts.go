package ai

// SystemPrompt is the base instruction for the Kubernetes assistant.
const SystemPrompt = `You are KubeNova's built-in Kubernetes assistant, embedded in a desktop Kubernetes IDE.
You help platform engineers understand, troubleshoot, secure and optimise Kubernetes clusters.

How to work:
- When a cluster is attached, use the provided tools to gather real evidence (resources, events, logs, metrics) before drawing conclusions. Prefer a few targeted tool calls over guessing.
- Cite the evidence you used (object names, event reasons, log lines, metric values).
- When you propose a fix, give the exact YAML patch or kubectl command. Wrap YAML in ` + "```yaml" + ` fences and shell in ` + "```bash" + ` fences so the IDE can offer one-click apply.
- Changes to the cluster only happen through write tools, which exist only when the user enabled them. Otherwise describe the change and let the user apply it.
- Never print secret values you encounter in full; refer to them by key name.
- Be concise: lead with the diagnosis or answer, then the supporting detail.`

// Task templates for one-click AI actions throughout the UI. The user's
// selected content is appended as context.
var Tasks = map[string]string{
	"explain-yaml":     "Explain this Kubernetes manifest: what it creates, how its parts relate, and any misconfiguration, security or reliability concern.",
	"generate-yaml":    "Generate production-ready Kubernetes YAML for the request below. Include resource requests/limits, probes, securityContext (non-root, read-only root filesystem where possible) and labels. Output only the YAML plus a short note on assumptions.",
	"troubleshoot-pod": "Troubleshoot this failing pod. Inspect its spec, status, events, previous and current logs, and its owner. Identify the root cause and give the concrete fix.",
	"analyze-events":   "Analyse these Kubernetes events. Group related ones, explain what is happening, rank by severity and suggest actions.",
	"explain-logs":     "Explain these logs: summarise what the application is doing, highlight errors and their likely causes, and suggest next steps.",
	"kubectl":          "Produce the kubectl command(s) that accomplish the request below. Explain each flag briefly. Prefer safe, non-destructive variants and mention --dry-run where relevant.",
	"explain-alert":    "Explain this Prometheus/Alertmanager alert: what the expression measures, why it fired, likely causes in this cluster and how to remediate. Query metrics if useful.",
	"explain-promql":   "Explain this PromQL query step by step and suggest improvements.",
	"explain-logql":    "Explain this Loki LogQL query step by step, describe what it returns and suggest improvements or alternatives.",
	"explain-helm":     "Explain this Helm chart / release: what it deploys, the important values, and risks in the current values. Suggest value changes for production.",
	"flux-failure":     "Explain why this Flux object is failing to reconcile. Inspect its status conditions, source, dependencies and related events, then give the fix.",
	"argocd-failure":   "Explain why this Argo CD application is failing to sync or is degraded. Inspect operation state, resource statuses and events, then give the fix.",
	"cost":             "Perform a cost optimisation review. Compare requests/limits against actual usage, find over-provisioned workloads, idle resources, unattached volumes and unused load balancers. Quantify savings where possible.",
	"security":         "Perform a security review: RBAC over-permissions, privileged/root containers, missing securityContext, host namespaces, secrets in env vars, missing NetworkPolicies, images using :latest. Rank findings and give fixes.",
	"dashboard":        "Design a monitoring dashboard for the request below. Respond ONLY with JSON of the form {\"title\":string,\"panels\":[{\"title\":string,\"type\":\"line\"|\"stat\"|\"table\",\"query\":PromQL string,\"unit\":\"cores\"|\"bytes\"|\"percent\"|\"rps\"|\"\"}]}.",
	"chat":             "",
}
