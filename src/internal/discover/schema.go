package discover

import (
	"encoding/json"
	"time"
)

type ServiceSchema struct {
	Port       uint16       `json:"port"`
	Protocol   string       `json:"protocol"`
	Source     string       `json:"source"`
	Stub       *OpenAPIStub `json:"stub,omitempty"`
	OpenAPIRaw []byte       `json:"-"`
}

type DiscoveryResult struct {
	Slug         string          `json:"slug"`
	DiscoveredAt time.Time       `json:"discovered_at"`
	Services     []ServiceSchema `json:"services"`
}

func BuildResult(targets []ServiceTarget, slug string) *DiscoveryResult {
	result := &DiscoveryResult{
		Slug:         slug,
		DiscoveredAt: time.Now().UTC(),
		Services:     make([]ServiceSchema, 0, len(targets)),
	}
	for _, t := range targets {
		result.Services = append(result.Services, buildServiceSchema(t, slug))
	}
	return result
}

func buildServiceSchema(t ServiceTarget, slug string) ServiceSchema {
	if t.Protocol == "http" || t.Protocol == "https" {
		doc, err := ProbeOpenAPI(t.Host, t.Port, t.Protocol == "https")
		if err == nil && doc != nil {
			return ServiceSchema{Port: t.Port, Protocol: t.Protocol, Source: "openapi"}
		}
	}
	return ServiceSchema{Port: t.Port, Protocol: t.Protocol, Source: "stub", Stub: GenerateStub(t, slug)}
}

func (r *DiscoveryResult) MarshalToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
