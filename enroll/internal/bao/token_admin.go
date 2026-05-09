package bao

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TokenListEntry is a token record paired with its identifier.
type TokenListEntry struct {
	Token  string
	Record TokenRecord
}

// ListTokens returns all known tokens. If includeAll is false, expired or
// consumed tokens are filtered out and only active tokens are returned.
func (c *httpClient) ListTokens(ctx context.Context, includeAll bool) ([]TokenListEntry, error) {
	keys, err := c.ListKeys(ctx, c.prefix+"/")
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	now := time.Now().UTC()
	var out []TokenListEntry
	for _, k := range keys {
		token := strings.TrimSuffix(k, "/")
		body, err := c.ReadJSON(ctx, c.prefix+"/"+token)
		if err != nil {
			continue
		}
		var rec TokenRecord
		if err := json.Unmarshal(body, &rec); err != nil {
			continue
		}
		if !includeAll {
			if rec.IsExpired(now) || rec.IsConsumed() {
				continue
			}
		}
		out = append(out, TokenListEntry{Token: token, Record: rec})
	}
	return out, nil
}

// DeleteToken removes the token record from Bao. Used to revoke unconsumed
// tokens. Calling on a consumed token also works (the consumption record is
// destroyed); callers should refuse to delete consumed tokens at the
// handler layer.
func (c *httpClient) DeleteToken(ctx context.Context, token string) error {
	return c.DeleteKey(ctx, c.prefix+"/"+token)
}
