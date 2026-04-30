package discover

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pb33f/libopenapi"
)

var wellKnownPaths = []string{
	"/openapi.json", "/openapi.yaml", "/swagger.json", "/swagger.yaml",
	"/api-docs", "/api/docs", "/docs/openapi.json",
	"/.well-known/openapi.json", "/api/v1/openapi.json", "/v1/openapi.json",
}

func ProbeOpenAPI(host string, port uint16, tls bool) (*libopenapi.Document, error) {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, host, port)

	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, path := range wellKnownPaths {
		resp, err := client.Get(base + path)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		ct := resp.Header.Get("Content-Type")
		if ct != "" &&
			ct != "application/json" && ct != "application/yaml" &&
			ct != "text/yaml" && ct != "text/plain" &&
			ct != "text/plain; charset=utf-8" {
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			continue
		}

		doc, err := libopenapi.NewDocument(body)
		if err != nil {
			continue
		}
		return &doc, nil
	}
	return nil, nil
}
