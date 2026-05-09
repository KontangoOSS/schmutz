package ziti

import (
	"context"
	"fmt"
)

func (c *httpClient) ListIdentities(ctx context.Context, f IdentityFilter) ([]Identity, error) {
	var resp struct {
		Data []Identity `json:"data"`
	}
	if err := c.doJSON(ctx, "GET", "/edge/management/v1/identities?limit=500", nil, &resp); err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	var out []Identity
	for _, id := range resp.Data {
		if !filterIdentity(id, f) {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

func filterIdentity(id Identity, f IdentityFilter) bool {
	if f.HasRole != "" {
		found := false
		for _, r := range id.RoleAttributes {
			if r == f.HasRole {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.NameContains != "" && !contains(id.Name, f.NameContains) {
		return false
	}
	if f.HasTagKey != "" {
		v, ok := id.Tags[f.HasTagKey]
		if !ok {
			return false
		}
		if f.HasTagValue != "" && v != f.HasTagValue {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

func (c *httpClient) GetIdentity(ctx context.Context, name string) (*Identity, error) {
	id, err := c.lookupIdentityIDByName(ctx, name)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data Identity `json:"data"`
	}
	if err := c.doJSON(ctx, "GET", "/edge/management/v1/identities/"+id, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *httpClient) lookupIdentityIDByName(ctx context.Context, name string) (string, error) {
	ids, err := c.ListIdentities(ctx, IdentityFilter{})
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if id.Name == name {
			return id.ID, nil
		}
	}
	return "", fmt.Errorf("identity %q not found", name)
}

func (c *httpClient) UpdateIdentity(ctx context.Context, name string, req UpdateIdentityRequest) error {
	id, err := c.lookupIdentityIDByName(ctx, name)
	if err != nil {
		return err
	}
	body := map[string]interface{}{
		"roleAttributes": req.RoleAttributes,
	}
	if req.Tags != nil {
		body["tags"] = req.Tags
	}
	return c.doJSON(ctx, "PATCH", "/edge/management/v1/identities/"+id, body, nil)
}

func (c *httpClient) DeleteIdentityWithSSH(ctx context.Context, name string) error {
	id, err := c.lookupIdentityIDByName(ctx, name)
	if err != nil {
		return err
	}
	// Delete the identity. Ziti will refuse if there are dependent service-policies;
	// caller is expected to clean up the per-machine SSH service first via separate API.
	// Tracked-rollback variant lives in handlers.
	return c.doJSON(ctx, "DELETE", "/edge/management/v1/identities/"+id, nil, nil)
}

// RoleAttributes returns the role attributes for the named identity.
// Implements the RoleResolver interface for ConnContextWithResolver.
func (c *httpClient) RoleAttributes(ctx context.Context, name string) ([]string, error) {
	id, err := c.GetIdentity(ctx, name)
	if err != nil {
		return nil, err
	}
	return id.RoleAttributes, nil
}

func (c *httpClient) CreateBareIdentity(ctx context.Context, name string, roleAttrs []string, tags map[string]string) (*EnrollmentResult, error) {
	// Check if identity already exists — prevents duplicates on re-enrollment.
	var listResp struct {
		Data []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Enrollment struct {
				OTT struct{ JWT string `json:"jwt"` } `json:"ott"`
			} `json:"enrollment"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "GET",
		fmt.Sprintf("/edge/management/v1/identities?filter=name%%3D%%22%s%%22", name),
		nil, &listResp); err == nil && len(listResp.Data) > 0 {
		existing := listResp.Data[0]
		if existing.Enrollment.OTT.JWT != "" {
			// Existing identity still has an unused OTT — reuse it.
			return &EnrollmentResult{
				IdentityID:   existing.ID,
				IdentityName: existing.Name,
				JWT:          existing.Enrollment.OTT.JWT,
			}, nil
		}
		// Enrolled identity with no OTT — delete it so we can re-create cleanly.
		_ = c.doJSON(ctx, "DELETE", "/edge/management/v1/identities/"+existing.ID, nil, nil)
	}

	body := map[string]interface{}{
		"name":           name,
		"type":           "Default",
		"isAdmin":        false,
		"enrollment":     map[string]bool{"ott": true},
		"roleAttributes": roleAttrs,
		"tags":           tags,
	}
	var idResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/identities", body, &idResp); err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}
	var detail struct {
		Data struct {
			Enrollment struct {
				OTT struct {
					JWT string `json:"jwt"`
				} `json:"ott"`
			} `json:"enrollment"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "GET", "/edge/management/v1/identities/"+idResp.Data.ID, nil, &detail); err != nil {
		return nil, fmt.Errorf("read OTT: %w", err)
	}
	return &EnrollmentResult{
		IdentityID:   idResp.Data.ID,
		IdentityName: name,
		JWT:          detail.Data.Enrollment.OTT.JWT,
	}, nil
}
