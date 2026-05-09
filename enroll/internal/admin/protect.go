package admin

import "errors"

// BreakGlassIdentityName is the canonical Ziti identity name for the genesis
// admin. Created during bootstrap. Protected from modification via the API.
const BreakGlassIdentityName = "break-glass-admin"

// ErrBreakGlassProtected is returned when an operation tries to modify the
// break-glass identity through the API. Recovery requires direct controller
// SSH access.
var ErrBreakGlassProtected = errors.New("break-glass identity is protected, modify via direct controller SSH only")

func IsBreakGlassIdentity(name string) bool {
	return name == BreakGlassIdentityName
}

func EnsureNotBreakGlass(name string) error {
	if IsBreakGlassIdentity(name) {
		return ErrBreakGlassProtected
	}
	return nil
}
