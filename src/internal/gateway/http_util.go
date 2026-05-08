package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

func newGetRequest(ctx context.Context, url string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
}

func doHTTP(req *http.Request) ([]byte, int, error) {
	b, status, _, err := doHTTPWithType(req)
	return b, status, err
}

// doHTTPWithType is like doHTTP but also returns the Content-Type header.
func doHTTPWithType(req *http.Request) ([]byte, int, string, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return b, resp.StatusCode, resp.Header.Get("Content-Type"), err
}
