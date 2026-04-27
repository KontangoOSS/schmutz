package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

type failureEntry struct {
	Count     int       `json:"count"`
	LastError string    `json:"last_error,omitempty"`
	NextRetry time.Time `json:"next_retry,omitempty"`
}

type serviceRegistryEntry struct {
	Request *ServiceRequest `json:"request"`
	Failure *failureEntry   `json:"failure,omitempty"`
}

type registry struct {
	Services []*serviceRegistryEntry `json:"services"`
}

func loadRegistry(path string) (*registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read: %w", err)
	}
	var r registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("registry: parse: %w", err)
	}
	return &r, nil
}

func (r *registry) save(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (a *Agent) ReloadRegistry() error {
	path, err := a.root.AgentRegistry()
	if err != nil {
		return err
	}
	reg, err := loadRegistry(path)
	if err != nil {
		return err
	}
	for _, entry := range reg.Services {
		if err := a.StartService(entry.Request); err != nil {
			entry.Failure = &failureEntry{Count: 1, LastError: err.Error()}
			entry.Failure.NextRetry = a.retryCalc.nextRetryTime(1)
			a.retryManager.addFailedService(entry)
		}
	}
	return nil
}

func (a *Agent) saveRegistry() {
	path, err := a.root.AgentRegistry()
	if err != nil {
		return
	}
	r := &registry{}
	for _, svc := range a.services {
		if svc.request != nil {
			r.Services = append(r.Services, &serviceRegistryEntry{Request: svc.request})
		}
	}
	if err := r.save(path); err != nil {
		fmt.Printf("agent: save registry: %v\n", err)
	}
}
