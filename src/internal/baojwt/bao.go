// Package baojwt is the agent-side client for the bao-jwt flow:
//
//	approle login → OIDC mint → JWT login → /run/bao-token
//
// It also handles the install-time bundle: fetch from the controller's
// /api/bao-bundle, unwrap the response-wrapped secret_id, and persist
// /etc/schmutz/agent.json so the refresh loop can run unattended.
//
// All Bao calls go to the address embedded in the bundle (the agent
// has no choice about the path — the bundle is authoritative). Today
// that's bao.tango over the ziti overlay, reached on :443 ALPN.
package baojwt
