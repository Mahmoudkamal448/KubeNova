# Plugin SDK

A plugin is a directory (installed from a `.zip` or the marketplace) with a `plugin.json` manifest:

| Field | Purpose |
|---|---|
| `id`, `name`, `version`, `author`, `description` | metadata (`id` must be a simple slug) |
| `entry` | ES module loaded by the UI (optional) |
| `panels[]` | `{id, title, location: "sidebar" \| "resource-tab", kinds?}` |
| `dashboards[]` | declarative PromQL dashboards: `{id, title, panels: [{title, type: line\|stat, query, unit}]}` |
| `themes[]` | `{id, name, dark, colors: {bg, bg2, panel, fg, accent, …}}` (CSS variables) |
| `shortcuts[]` | `{keys, command}` |
| `aiTools[]` | `{name, description, properties, required, command? \| url?, write?}` |

## UI module

```js
const sdk = window.kubenovaSDK;
sdk.registerPanel("my.panel", (el, ctx) => {
  // ctx: { clusterId, namespace, resource?: {kind, name, namespace, object}, api }
  ctx.api.get(`/clusters/${ctx.clusterId}/r/core/v1/pods`).then((r) => (el.textContent = `${r.rows.length} pods`));
  return () => {/* cleanup */};
});
```

`ctx.api` is an authenticated client for the whole backend API (`get`, `post`, `put`, `patch`, `del`). Resource paths follow `/clusters/{id}/r/{group|core}/{version}/{resource}[/{name}]?namespace=`.

## Custom AI tools

Command tools run in the plugin directory with `KUBECONFIG` set to the active cluster. They receive the tool input as JSON on stdin and print the result to stdout. URL tools receive a JSON `POST`. Tools marked `"write": true` are only offered when the user allows the assistant to change clusters.

See [`examples/plugin-hello`](../examples/plugin-hello).

## Publishing

Add an entry to `marketplace/index.json` (`id, name, description, author, version, download, tags`). `download` must point to a zip whose root (or single top-level folder) contains `plugin.json`.
