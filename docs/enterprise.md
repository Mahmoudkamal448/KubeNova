# Enterprise features (included for free)

KubeNova has a single edition. Every feature other tools sell as "Pro" or "Enterprise" ships enabled for everyone, with no license key, seat limit or cluster limit. `GET /api/license` always reports `allUnlocked: true`. A license record is optional and only stores an organisation name for support.

## Team workspaces & RBAC
Workspaces group clusters, members and shared settings. Members are users (email/username) or `group:<name>` (from OIDC/SAML/LDAP group claims or SCIM groups) with the role `viewer`, `editor`, `admin` or `owner`. When **Enforce workspace RBAC** is on, the API checks every request to a cluster that belongs to a workspace: reads need `viewer`, changes need `editor`.

## Audit logs
Every mutating API call is appended to `audit/audit.jsonl`, along with exec/terminal session starts. Each entry records time, user, cluster, action, method, path, status and remote address. You can search it in the UI or export it as JSONL for your SIEM.

## Identity
* **OIDC**: authorization code + PKCE with a loopback redirect (`/api/auth/oidc/callback`). The ID token is verified against the issuer's JWKS (RS256, issuer, audience, nonce, expiry).
* **SAML 2.0**: built-in SP (`/api/auth/saml/saml/metadata`, ACS `/api/auth/saml/saml/acs`). The key pair is generated on first use.
* **LDAP / Active Directory**: service bind + user search + user bind; `memberOf` groups.
* **SCIM 2.0**: `/scim/v2/Users` and `/scim/v2/Groups` with bearer-token auth for Okta, Entra ID and OneLogin provisioning.

## Air-gapped mode
Turns off internet-dependent features: the assistant uses a local OpenAI-compatible model, the marketplace is disabled (install plugins from `.zip`), and external CLIs come from the bundled tools directory.

## Proxy
One proxy setting applies to Kubernetes API traffic, CLI tools (`HTTPS_PROXY`) and integrations. Per-cluster `proxy-url` in kubeconfig is honoured too.

## FIPS 140-3
Build the backend with the Go native FIPS module: `make fips` (`GOFIPS140=v1.0.0`), or `KUBENOVA_FIPS=1 npm run dist`. The UI shows whether FIPS mode is active.

## Team server mode
`kubenova-server --listen 0.0.0.0:7443 --token <secret>` serves the same API to a browser build of the UI. Put it behind TLS and your SSO provider.
