package agent

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeHTTP_ServesOverListener verifies that serveHTTP
// runs http.Serve on the provided listener with the given handler.
// Uses a real TCP listener (not Ziti) since we don't have a controller in tests.
func TestServeHTTP_ServesOverListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from gateway"))
	})

	h := &ZitiHost{stopCh: make(chan struct{})}
	h.listeners = append(h.listeners, ln)
	h.wg.Add(1)

	go h.serveHTTP(ln, "schmutz-api.tango", handler)

	// Give the goroutine a moment to start.
	time.Sleep(20 * time.Millisecond)

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d", resp.StatusCode)
	}
	if string(body) != "hello from gateway" {
		t.Errorf("body: got %q", body)
	}

	h.Stop()
}
