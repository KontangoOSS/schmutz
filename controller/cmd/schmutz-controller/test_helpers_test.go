package main

import (
	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

func newTestAPI() *API {
	return &API{
		ottStore: service.NewOTTStore(),
		cfg:      &Config{JoinDomain: "join.test.local"},
	}
}

// newTestStore returns a non-nil *StoreService with a nil client.
// Methods on StoreService guard against nil client so this is safe for tests
// that need a non-nil store to pass requireStore() but don't make real Bao calls.
func newTestStore() *service.StoreService {
	s, _ := service.NewStoreService("http://127.0.0.1:1", "test-token")
	return s
}
