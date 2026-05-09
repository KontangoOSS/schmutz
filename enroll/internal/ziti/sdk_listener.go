// server/internal/ziti/sdk_listener.go
package ziti

import (
	"context"
	"fmt"
	"net"

	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
	zsdk "github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// SDKListener wraps a Ziti SDK service-bind listener. The HTTP server reads
// the source identity off the underlying edge.Conn and stuffs it into the
// request's context via ConnContext.
type SDKListener struct {
	listener net.Listener
	ctx      zsdk.Context
}

// NewSDKListener loads the identity file, dials the controller, binds the
// given service name, and returns a listener ready for `http.Serve`.
func NewSDKListener(identityFile, serviceName string) (*SDKListener, error) {
	cfg, err := zsdk.NewConfigFromFile(identityFile)
	if err != nil {
		return nil, fmt.Errorf("load identity %s: %w", identityFile, err)
	}
	zctx, err := zsdk.NewContext(cfg)
	if err != nil {
		return nil, fmt.Errorf("new ziti context: %w", err)
	}
	if err := zctx.Authenticate(); err != nil {
		return nil, fmt.Errorf("ziti authenticate: %w", err)
	}
	ln, err := zctx.Listen(serviceName)
	if err != nil {
		return nil, fmt.Errorf("ziti listen %s: %w", serviceName, err)
	}
	return &SDKListener{listener: ln, ctx: zctx}, nil
}

// Listener returns the net.Listener for use with http.Server.Serve.
func (s *SDKListener) Listener() net.Listener { return s.listener }

// Close shuts down the listener and the SDK context.
func (s *SDKListener) Close() error {
	err := s.listener.Close()
	s.ctx.Close()
	return err
}

// ConnContext is wired into http.Server.ConnContext so each accepted
// connection populates caller identity in the context that handlers see.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	ec, ok := c.(edge.Conn)
	if !ok {
		return ctx
	}
	srcID := ec.SourceIdentifier()
	if srcID == "" {
		return ctx
	}
	// SDK gives us the identity name. Role attributes require a separate
	// management-API lookup, which ConnContextWithResolver performs per
	// connection (no cache yet — see RoleResolver).
	caller := identity.Caller{Name: srcID}
	return identity.WithCaller(ctx, caller)
}

// RoleResolver looks up the role attributes for a Ziti identity name.
// Used to enrich caller context after the connection is accepted.
type RoleResolver interface {
	RoleAttributes(ctx context.Context, name string) ([]string, error)
}

// ConnContextWithResolver returns a ConnContext function that enriches the
// caller with role attributes via the given resolver. Resolution failures
// fall back to a Caller with no roles (RBAC middleware will then 403).
func ConnContextWithResolver(resolver RoleResolver) func(context.Context, net.Conn) context.Context {
	return func(ctx context.Context, c net.Conn) context.Context {
		ec, ok := c.(edge.Conn)
		if !ok {
			return ctx
		}
		name := ec.SourceIdentifier()
		if name == "" {
			return ctx
		}
		caller := identity.Caller{Name: name}
		if attrs, err := resolver.RoleAttributes(ctx, name); err == nil {
			caller.RoleAttributes = attrs
		}
		return identity.WithCaller(ctx, caller)
	}
}
