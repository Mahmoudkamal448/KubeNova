# Architecture

## Processes

* **Electron main** (`frontend/electron/main.ts`) starts `kubenova-server --listen 127.0.0.1:0`. It reads the `KUBENOVA_READY {"url","token"}` line from stdout and passes it to the sandboxed renderer through the preload bridge. The window runs with `contextIsolation`, no Node integration, and a strict CSP.
* **Renderer** (React + TypeScript + Zustand) talks to the backend over HTTP (`X-KubeNova-Token` header) and WebSockets (`?token=`) for watches, logs, exec, terminal and AI streaming.
* **Backend** (Go) is a single static binary. The same binary also runs as a shared team server (`--listen 0.0.0.0:7443 --token …`) and as an MCP server (`--mcp`).

## Kubernetes access

* `clusters.Manager` loads every context from every kubeconfig source. It builds `rest.Config`s with `clientcmd`, which gives exec plugins (AWS, GKE, kubelogin, OIDC), auth providers, client certs, tokens and `proxy-url` support. It also caches the typed, dynamic, memory-cached discovery and deferred REST mapper clients for each cluster.
* `resources.Service` is fully generic: it uses discovery for types and the dynamic client for objects. Lists are paginated (`limit`/`continue`) and returned as compact `Row`s with kind-specific columns. The UI then opens a watch from the list's `resourceVersion` and receives row-level events, batched every 250 ms. A `RELIST` event handles "resource version too old".
* Diff uses a server-side apply **dry run**, so admission webhooks and defaulting are reflected before an LCS-based unified diff is computed.
* Exec uses the WebSocket executor with SPDY fallback. Port-forward uses SPDY. File copy streams tar over exec, like `kubectl cp`.

## Scale

* Health checks run in the background with a configurable worker pool (default 16) and per-cluster timeouts.
* Multi-cluster search fans out with a semaphore of 16.
* The offline cache persists the last list of every type/namespace and the API discovery document under `<data dir>/cache/<cluster>`. When a cluster is unreachable, the UI shows cached rows marked *offline*.
* Tables are virtualized with `@tanstack/react-virtual`, so 100k rows render smoothly.

## AI

`ai.Assistant` runs a manual tool-use loop with the official Anthropic Go SDK (default model `claude-opus-5-5`, server-side refusal fallback enabled). Tools are assembled per request from:

1. Built-in Kubernetes tools (`internal/ai/k8stools.go`). Mutating tools are only offered when *Allow writes* is on.
2. Tools from connected MCP servers (`mcp__<server>__<tool>`).
3. Custom AI tools contributed by plugins (command or HTTP).

Progress steps stream to the UI over `/api/ai/stream`.

## Storage

All state is JSON under the user config dir (`KubeNova/`): settings, cluster metadata, Rancher servers, connections, workspaces, identity config, caches, plugins, and the append-only `audit/audit.jsonl`. Files are written atomically with mode 0600.
