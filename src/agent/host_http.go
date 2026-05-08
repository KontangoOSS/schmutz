package agent

import (
	"fmt"
	"log"
	"net"
	"net/http"
)

// BindHTTPService binds a Ziti service by name and serves the given HTTP
// handler directly on the Ziti listener. Unlike BindServices (which forwards
// raw TCP to a local port), this method runs http.Serve on the listener —
// the agent IS the HTTP server, no local upstream needed.
//
// Used for schmutz-api.{zone}: the gateway's http.Handler is served directly
// over the Ziti overlay without any localhost port involvement.
func (h *ZitiHost) BindHTTPService(serviceName string, handler http.Handler) error {
	listener, err := h.ctx.Listen(serviceName)
	if err != nil {
		return fmt.Errorf("ziti host: listen %q: %w", serviceName, err)
	}
	h.listeners = append(h.listeners, listener)
	h.wg.Add(1)
	go h.serveHTTP(listener, serviceName, handler)
	log.Printf("ziti host: bound %q → HTTP handler", serviceName)
	return nil
}

// serveHTTP runs http.Serve on the listener. It exits cleanly when Stop()
// closes the listener, or logs errors for other failures.
func (h *ZitiHost) serveHTTP(ln net.Listener, serviceName string, handler http.Handler) {
	defer h.wg.Done()
	srv := &http.Server{Handler: handler}
	if err := srv.Serve(ln); err != nil {
		select {
		case <-h.stopCh:
			// clean shutdown — expected when Stop() is called
		default:
			log.Printf("ziti host: %s: http serve: %v", serviceName, err)
		}
	}
}
