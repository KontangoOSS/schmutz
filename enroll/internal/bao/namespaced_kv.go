package bao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NamespacedKV writes/reads JSON in a non-root Bao namespace. Used by
// the controller-side admin tool to persist per-deployment substrate at
//   <namespace>/secret/data/apps/<app>/<deployment>/substrate
//
// The existing AdminKV writes to the root namespace via the configured
// mount; namespacing requires the X-Vault-Namespace header. Rather than
// thread a namespace through every AdminKV call, this is a separate
// narrow surface for namespaced ops.
type NamespacedKV interface {
	// WriteJSON writes body to <namespace>/<mount>/data/<path>.
	// mount is typically "secret" for KV v2.
	WriteJSON(ctx context.Context, namespace, mount, path string, body []byte) error

	// ReadJSON reads <namespace>/<mount>/data/<path>. Returns ErrNotFound
	// if the path doesn't exist.
	ReadJSON(ctx context.Context, namespace, mount, path string) ([]byte, error)
}

// NewNamespacedKV builds a NamespacedKV using the same HTTP transport as
// the rest of internal/bao. authToken needs cross-namespace write
// capability (typically a root admin token).
func NewNamespacedKV(addr, authToken string, skipTLSVerify bool) NamespacedKV {
	cli := NewHTTP(addr, authToken, "secret", "ignored", skipTLSVerify).(*httpClient)
	return &namespacedHTTP{c: cli}
}

type namespacedHTTP struct {
	c *httpClient
}

func (n *namespacedHTTP) WriteJSON(ctx context.Context, namespace, mount, path string, body []byte) error {
	wrapped, err := json.Marshal(map[string]any{"data": json.RawMessage(body)})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/v1/%s/data/%s", n.c.addr, mount, path)
	status, b, err := n.doNamespaced(ctx, "POST", namespace, url, wrapped)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao write %s/%s: status %d: %s", namespace, path, status, b)
	}
	return nil
}

func (n *namespacedHTTP) ReadJSON(ctx context.Context, namespace, mount, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s", n.c.addr, mount, path)
	status, body, err := n.doNamespaced(ctx, "GET", namespace, url, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("bao read %s/%s: status %d: %s", namespace, path, status, body)
	}
	var kv kvV2Response
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, fmt.Errorf("decode kv: %w", err)
	}
	return kv.Data.Data, nil
}

// doNamespaced mirrors httpClient.do but injects the X-Vault-Namespace
// header. The existing httpClient.do doesn't accept extra headers; we
// keep it that way and live with the small duplication here.
func (n *namespacedHTTP) doNamespaced(ctx context.Context, method, namespace, url string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Vault-Token", n.c.authToken)
	if namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := n.c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("bao request failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}
