package agent

import (
	"log/slog"
	"strings"
)

// ServiceEvent represents a Ziti service change.
// This mirrors what the Ziti SDK provides but is testable without the SDK.
type ServiceEvent struct {
	Name  string
	Added bool // true = added, false = removed
}

// HandleServiceChange processes a service event and updates the daemon state.
// Called by the SDK event listeners when they fire.
func (d *Daemon) HandleServiceChange(event ServiceEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if event.Added {
		switch {
		case strings.Contains(event.Name, "-api"):
			slog.Info("API service available", "service", event.Name)
		case strings.Contains(event.Name, "-ssh"):
			slog.Info("SSH service available", "service", event.Name)
		default:
			slog.Info("service available", "service", event.Name)
		}
		d.services[event.Name] = true
	} else {
		slog.Info("service removed", "service", event.Name)
		delete(d.services, event.Name)
	}
}
