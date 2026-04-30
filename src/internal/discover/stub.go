package discover

import "fmt"

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type OpenAPIStub struct {
	OpenAPI  string                 `json:"openapi"`
	Info     OpenAPIInfo            `json:"info"`
	Servers  []OpenAPIServer        `json:"servers"`
	XSchmutz map[string]interface{} `json:"x-schmutz,omitempty"`
}

var protocolMeta = map[string]struct{ title, desc string }{
	"ssh":        {"SSH Service", "Secure Shell access"},
	"http":       {"HTTP Service", "HTTP web service (no OpenAPI spec found)"},
	"https":      {"HTTPS Service", "HTTPS web service (no OpenAPI spec found)"},
	"postgresql": {"PostgreSQL Database", "Relational database service"},
	"mysql":      {"MySQL Database", "Relational database service"},
	"redis":      {"Redis Cache", "In-memory data structure store"},
	"mongodb":    {"MongoDB Database", "Document-oriented database service"},
	"smtp":       {"SMTP Mail", "Mail transfer agent"},
	"dns":        {"DNS Service", "Domain name resolution service"},
	"kafka":      {"Kafka Broker", "Distributed event streaming"},
	"mqtt":       {"MQTT Broker", "Message queue telemetry transport"},
}

func GenerateStub(target ServiceTarget, slug string) *OpenAPIStub {
	meta, ok := protocolMeta[target.Protocol]
	if !ok {
		meta = struct{ title, desc string }{
			title: fmt.Sprintf("%s Service (port %d)", target.Protocol, target.Port),
			desc:  fmt.Sprintf("Service fingerprinted as '%s' on port %d", target.Protocol, target.Port),
		}
	}
	return &OpenAPIStub{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       fmt.Sprintf("%s — %s", slug, meta.title),
			Version:     "0.0.0",
			Description: meta.desc,
		},
		Servers: []OpenAPIServer{{URL: fmt.Sprintf("http://%s:%d", target.Host, target.Port), Description: "Local service"}},
		XSchmutz: map[string]interface{}{
			"discovered_by": "schmutz",
			"protocol":      target.Protocol,
			"port":          target.Port,
			"source":        "fingerprint",
		},
	}
}
