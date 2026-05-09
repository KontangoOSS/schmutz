package api

// Compile-time interface compliance checks.
//
// These verify that the concrete service types satisfy the key sub-interfaces
// that the HTTP handlers actually depend on today. As the service layer
// matures, the full BaoStore / ZitiAdmin / IdentityMgr interfaces will be
// wired in here and the sub-interfaces removed.

import "git.konoss.org/kore/schmutz/controller/internal/service"

// BaoKVStore covers the KV methods used by /api/bao/kv/* handlers.
// A subset of BaoStore that StoreService fully implements today.
type BaoKVStore interface {
	Get(mount, path string) (map[string]interface{}, error)
	Put(mount, path string, data map[string]interface{}) error
	Delete(mount, path string) error
	List(mount, path string) ([]string, error)
}

var (
	_ BaoKVStore = (*service.StoreService)(nil)
	// ZitiService and IdentityService compliance will be added here as
	// the full interface methods are aligned with concrete implementations.
)
