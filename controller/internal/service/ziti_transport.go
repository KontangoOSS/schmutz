package service

import (
	"context"
	stdtls "crypto/tls"
	"log"
	"net"
	"net/http"
	"time"

	sdkhttp "github.com/openziti/sdk-golang"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// ZitiTransport provides Ziti SDK-based networking for the controller.
// It can both dial Ziti services (outbound) and host services (inbound).
type ZitiTransport struct {
	ctx     ziti.Context
	options *ziti.Config
}

// NewZitiTransport creates a transport from a Ziti identity file.
func NewZitiTransport(identityPath string) (*ZitiTransport, error) {
	cfg, err := ziti.NewConfigFromFile(identityPath)
	if err != nil {
		return nil, err
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		return nil, err
	}
	return &ZitiTransport{ctx: ctx, options: cfg}, nil
}

// Context returns the underlying Ziti context.
func (t *ZitiTransport) Context() ziti.Context {
	return t.ctx
}

// zitiServiceMap maps URL addresses to Ziti service names.
// When the transport sees one of these addresses it dials the Ziti service
// instead of resolving DNS — this avoids the Go resolver firing on .tango names.
var zitiServiceMap = map[string]string{
	// Management API: Ziti overlay already encrypts — don't double-TLS
	// The host config points to localhost:1280 on the controller
	"ziti-mgmt.tango:443": "ziti-mgmt.tango",
	"bao.tango:8200":      "bao.tango",
	// nats.tango removed: telemetry now uses telemetry.tango over Ziti TCP frames, not NATS
}

// HTTPTransport returns an http.Transport that dials through Ziti.
// Addresses in zitiServiceMap are routed through the overlay without DNS.
// All other addresses fall back to regular TCP/TLS.
func (t *ZitiTransport) HTTPTransport() *http.Transport {
	dialZitiOrFallback := func(ctx context.Context, network, addr string, tls bool) (net.Conn, error) {
		if svc, ok := zitiServiceMap[addr]; ok {
			conn, err := t.ctx.Dial(svc)
			if err == nil {
				return conn, nil
			}
			log.Printf("ziti: failed to dial %s via overlay: %v — falling back to direct", svc, err)
		}
		if tls {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, addr)
		}
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, network, addr)
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialZitiOrFallback(ctx, network, addr, false)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if svc, ok := zitiServiceMap[addr]; ok {
				conn, err := t.ctx.Dial(svc)
				if err == nil {
					// Wrap with TLS — the controller's cert is self-signed so skip verify
					host, _, _ := net.SplitHostPort(addr)
					tlsConn := stdtls.Client(conn, &stdtls.Config{
						ServerName:         host,
						InsecureSkipVerify: true,
					})
					if err := tlsConn.HandshakeContext(ctx); err != nil {
						conn.Close()
						return nil, err
					}
					return tlsConn, nil
				}
				log.Printf("ziti: failed to dial %s via overlay: %v — falling back to direct", svc, err)
			}
			dialer := &stdtls.Dialer{Config: &stdtls.Config{InsecureSkipVerify: true}}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}

// HTTPClient returns an http.Client that routes through Ziti.
// Uses our custom transport which dials via the Ziti overlay then wraps TLS —
// needed because management API endpoints speak TLS over the Ziti connection.
func (t *ZitiTransport) HTTPClient() *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: t.HTTPTransport(),
	}
}

// SDKHTTPClient returns the SDK's native Ziti HTTP client.
// Use this for plain-HTTP services on the overlay (no double-TLS).
func (t *ZitiTransport) SDKHTTPClient() *http.Client {
	return sdkhttp.NewHttpClient(t.ctx, &stdtls.Config{InsecureSkipVerify: true})
}

// Listen creates a Ziti service listener for hosting the controller API.
// The returned net.Listener can be passed to http.Serve().
func (t *ZitiTransport) Listen(serviceName string) (net.Listener, error) {
	listener, err := t.ctx.Listen(serviceName)
	if err != nil {
		return nil, err
	}
	log.Printf("ziti: listening on service %q", serviceName)
	return listener, nil
}

// ListenEdge creates a Ziti service listener that returns edge.Conn on Accept,
// giving access to GetDialerIdentityName() and GetDialerIdentityId().
func (t *ZitiTransport) ListenEdge(serviceName string) (edge.Listener, error) {
	listener, err := t.ctx.Listen(serviceName)
	if err != nil {
		return nil, err
	}
	log.Printf("ziti: edge listener on service %q", serviceName)
	return listener, nil
}

// Close shuts down the Ziti context.
func (t *ZitiTransport) Close() {
	t.ctx.Close()
}
