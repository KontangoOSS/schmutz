package agent

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/openziti/sdk-golang/ziti"
)

// servicePort maps a known service name prefix to its local forwarding address.
var servicePort = map[string]string{
	"ssh-":   "127.0.0.1:22",
	"http-":  "127.0.0.1:80",
	"https-": "127.0.0.1:443",
}

// localPortForService returns the local address to forward connections to
// for the given Ziti service name. Service names follow the pattern
// "<protocol>-<slug>" (e.g. "ssh-web-1"). Returns ok=false for unknown prefixes.
func localPortForService(serviceName string) (addr string, ok bool) {
	for prefix, port := range servicePort {
		if strings.HasPrefix(serviceName, prefix) {
			return port, true
		}
	}
	return "", false
}

// ZitiHost holds a live Ziti SDK context and the set of active service listeners.
// It replaces the `ziti tunnel host` subprocess entirely.
type ZitiHost struct {
	ctx       ziti.Context
	listeners []net.Listener
	wg        sync.WaitGroup
	stopOnce  sync.Once
	stopCh    chan struct{}
}

// NewZitiHost loads the Ziti identity from identityPath and returns a ZitiHost
// ready to bind services. Does not start any listeners yet.
func NewZitiHost(identityPath string) (*ZitiHost, error) {
	cfg, err := ziti.NewConfigFromFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("ziti host: load identity %s: %w", identityPath, err)
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		return nil, fmt.Errorf("ziti host: create context: %w", err)
	}
	return &ZitiHost{
		ctx:    ctx,
		stopCh: make(chan struct{}),
	}, nil
}

// BindServices calls ctx.Listen for each service name in the list and starts
// forwarding connections to the corresponding local port.
// Services with unknown prefixes are logged and skipped.
// Returns an error only if zero services could be bound.
func (h *ZitiHost) BindServices(serviceNames []string) error {
	bound := 0
	for _, name := range serviceNames {
		localAddr, ok := localPortForService(name)
		if !ok {
			log.Printf("ziti host: skipping unknown service %q (no local port mapping)", name)
			continue
		}
		listener, err := h.ctx.Listen(name)
		if err != nil {
			log.Printf("ziti host: listen %q: %v (skipping)", name, err)
			continue
		}
		h.listeners = append(h.listeners, listener)
		h.wg.Add(1)
		go h.serveListener(listener, name, localAddr)
		log.Printf("ziti host: bound %q → %s", name, localAddr)
		bound++
	}
	if bound == 0 {
		h.ctx.Close()
		return fmt.Errorf("ziti host: no services could be bound from %v", serviceNames)
	}
	return nil
}

// serveListener accepts connections on a Ziti service listener and forwards
// each to the local target address.
func (h *ZitiHost) serveListener(listener net.Listener, serviceName, localAddr string) {
	defer h.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-h.stopCh:
				return // clean shutdown
			default:
				log.Printf("ziti host: %s: accept: %v", serviceName, err)
				return
			}
		}
		h.wg.Add(1)
		go h.forward(conn, serviceName, localAddr)
	}
}

// forward copies data bidirectionally between the Ziti connection and the local port.
func (h *ZitiHost) forward(zitiConn net.Conn, serviceName, localAddr string) {
	defer h.wg.Done()
	defer zitiConn.Close()

	local, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("ziti host: %s: dial local %s: %v", serviceName, localAddr, err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() { io.Copy(local, zitiConn); done <- struct{}{} }()
	go func() { io.Copy(zitiConn, local); done <- struct{}{} }()
	<-done
}

// Stop closes all listeners and the Ziti context, then waits for all
// in-flight connections to drain.
func (h *ZitiHost) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
		for _, l := range h.listeners {
			l.Close()
		}
		if h.ctx != nil {
			h.ctx.Close()
		}
	})
	h.wg.Wait()
}
