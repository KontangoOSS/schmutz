package agent

import (
	"fmt"
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
