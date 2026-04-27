package agent

import (
	"fmt"
	"net"
	"os"

	"github.com/KontangoOSS/schmutz/root"
)

type AgentConfig struct {
	RetryMinDelay int // seconds
	RetryMaxDelay int // seconds
	RetryMaxCount int
}

func DefaultConfig() *AgentConfig {
	return &AgentConfig{
		RetryMinDelay: 5,
		RetryMaxDelay: 600,
		RetryMaxCount: 0, // 0 = retry forever
	}
}

type Agent struct {
	cfg          *AgentConfig
	root         root.Root
	agentSocket  string
	services     map[string]*service
	addService   chan *service
	rmService    chan *service
	retryManager *retryManager
	retryCalc    *retryCalculator
	persist      bool
	quit         chan struct{}
}

func NewAgent(cfg *AgentConfig, r root.Root) (*Agent, error) {
	if !r.IsEnabled() {
		return nil, fmt.Errorf("device not enrolled; run 'schmutz enroll' first")
	}
	a := &Agent{
		cfg:        cfg,
		root:       r,
		services:   make(map[string]*service),
		addService: make(chan *service, 16),
		rmService:  make(chan *service, 16),
		quit:       make(chan struct{}),
	}
	a.retryCalc = newRetryCalculator(cfg)
	a.retryManager = newRetryManager(a)
	return a, nil
}

func (a *Agent) Run() error {
	sock, err := a.root.AgentSocket()
	if err != nil {
		return err
	}
	os.Remove(sock)
	l, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("agent: listen %s: %w", sock, err)
	}
	a.agentSocket = sock

	go a.retryManager.run()
	go a.manager()

	a.persist = false
	if err := a.ReloadRegistry(); err != nil {
		fmt.Printf("agent: reload registry: %v\n", err)
	}
	a.persist = true

	// Block until shutdown
	<-a.quit
	l.Close()
	return nil
}

func (a *Agent) Shutdown() {
	a.persist = false
	sock, _ := a.root.AgentSocket()
	os.Remove(sock)
	a.retryManager.stop()
	close(a.quit)
}

func (a *Agent) manager() {
	for {
		select {
		case svc := <-a.addService:
			a.services[svc.name] = svc
			if a.persist {
				a.saveRegistry()
			}

		case svc := <-a.rmService:
			if found, ok := a.services[svc.name]; ok {
				found.stop()
				delete(a.services, svc.name)

				// retry on abnormal exit
				if svc.hostExited && !svc.releaseRequested {
					a.retryManager.addFailedService(&serviceRegistryEntry{
						Request: svc.request,
						Failure: &failureEntry{Count: 1},
					})
				}

				if a.persist {
					a.saveRegistry()
				}
			}

		case <-a.quit:
			// Stop all running services on shutdown
			for _, svc := range a.services {
				svc.releaseRequested = true
				svc.stop()
			}
			return
		}
	}
}
