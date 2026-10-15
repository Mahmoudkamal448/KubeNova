// Example KubeNova UI plugin. `window.kubenovaSDK` provides:
//   api              authenticated backend client (get/post/put/patch/del)
//   registerPanel    (id, (element, ctx) => cleanup?) — ctx has clusterId, namespace, resource
const sdk = window.kubenovaSDK;

sdk.registerPanel("hello.summary", (el, ctx) => {
  if (!ctx.clusterId) {
    el.textContent = "Select a cluster.";
    return;
  }
  el.textContent = "Loading…";
  const ns = ctx.namespace ? `?namespace=${ctx.namespace}` : "";
  Promise.all([
    ctx.api.get(`/clusters/${ctx.clusterId}/r/core/v1/pods${ns}`),
    ctx.api.get(`/clusters/${ctx.clusterId}/r/apps/v1/deployments${ns}`),
  ]).then(([pods, deps]) => {
    const bad = pods.rows.filter((r) => r.health === "error");
    el.innerHTML = `<h3>Namespace ${ctx.namespace || "(all)"}</h3>
      <p>${deps.rows.length} deployments · ${pods.rows.length} pods · <b style="color:var(--err)">${bad.length} failing</b></p>
      <ul>${bad.slice(0, 20).map((r) => `<li>${r.namespace}/${r.name}: ${r.status}</li>`).join("")}</ul>`;
  });
});

sdk.registerPanel("hello.deploy-images", (el, ctx) => {
  const containers = ctx.resource?.object?.spec?.template?.spec?.containers || [];
  el.innerHTML = `<table class="tbl small"><thead><tr><th>Container</th><th>Image</th><th>Pull policy</th></tr></thead><tbody>${containers
    .map((c) => `<tr><td>${c.name}</td><td class="mono">${c.image}</td><td>${c.imagePullPolicy || "-"}</td></tr>`)
    .join("")}</tbody></table>`;
});
