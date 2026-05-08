package agent

import (
	"fmt"
	"net/http"
)

// StartService creates a ZitiHost for the given ServiceRequest, binds all
// listed Ziti services, and registers the host with the agent manager.
//
// Service names in req.Services must follow the "<protocol>-<slug>" pattern
// (e.g. "ssh-web-1"). Unknown prefixes are skipped with a log line.
// Returns an error if no services could be bound.
func (a *Agent) StartService(req *ServiceRequest) error {
	identityPath, err := a.root.IdentityPath()
	if err != nil {
		return err
	}

	host, err := NewZitiHost(identityPath)
	if err != nil {
		return fmt.Errorf("start service %s: %w", req.Name, err)
	}

	if err := host.BindServices(req.Services); err != nil {
		return fmt.Errorf("start service %s: %w", req.Name, err)
	}

	svc := &service{
		name:    req.Name,
		request: req,
		host:    host,
		agent:   a,
	}

	go svc.monitor()
	a.addService <- svc
	return nil
}

// StartHTTPService creates a ZitiHost, binds the named Ziti service, and
// serves the given HTTP handler directly on the Ziti listener.
// Used by the embedded gateway to expose schmutz-api.{zone} over the overlay.
func (a *Agent) StartHTTPService(serviceName string, handler http.Handler) error {
	identityPath, err := a.root.IdentityPath()
	if err != nil {
		return err
	}
	host, err := NewZitiHost(identityPath)
	if err != nil {
		return fmt.Errorf("start http service %s: %w", serviceName, err)
	}
	if err := host.BindHTTPService(serviceName, handler); err != nil {
		return fmt.Errorf("start http service %s: %w", serviceName, err)
	}
	svc := &service{
		name:  serviceName,
		host:  host,
		agent: a,
	}
	go svc.monitor()
	a.addService <- svc
	return nil
}
