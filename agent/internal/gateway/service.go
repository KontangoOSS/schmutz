package gateway

import "time"

// ServiceEntry is one discovered local service with its resolved OpenAPI spec.
type ServiceEntry struct {
	Name          string    `json:"name"`
	Port          uint16    `json:"port"`
	Protocol      string    `json:"protocol"`
	SpecSource    string    `json:"spec_source"`
	SpecJSON      []byte    `json:"-"`
	EndpointCount int       `json:"endpoints,omitempty"`
	DiscoveredAt  time.Time `json:"discovered_at"`
}
