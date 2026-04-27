package agent

// ServiceRequest describes the overlay tunnel to run.
// Name is informational. Services lists the Ziti service names to bind
// (returned by the enrollment identity event).
type ServiceRequest struct {
	Name     string   `json:"name"`
	Services []string `json:"services"`
}

type service struct {
	name    string
	request *ServiceRequest

	releaseRequested bool
	hostExited       bool
	lastError        error

	host  *ZitiHost
	agent *Agent
}

// stop cleanly shuts down the ZitiHost for this service.
func (s *service) stop() {
	if s.host != nil {
		s.host.Stop()
	}
}

// monitor blocks until the ZitiHost's stopCh is closed (i.e. Stop() was called),
// then signals the agent to remove this service entry.
func (s *service) monitor() {
	if s.host != nil {
		<-s.host.stopCh
	}
	s.hostExited = true
	s.agent.rmService <- s
}
