// Package common holds shared types and SDK client references.
// Every controller module depends on this — nothing else.
package common

import (
	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// Clients holds references to the SDK clients. Passed to every module.
type Clients struct {
	Bao      *service.StoreService
	Ziti     *service.ZitiService
	Identity *service.IdentityService
}
