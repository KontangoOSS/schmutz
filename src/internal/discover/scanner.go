package discover

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/praetorian-inc/fingerprintx/pkg/plugins"
	"github.com/praetorian-inc/fingerprintx/pkg/scan"
)

var CommonPorts = []uint16{
	22, 80, 443, 3000, 3306, 5432, 6379, 8080, 8443, 8888, 9090, 9200, 27017,
}

type ServiceTarget struct {
	Port     uint16
	Protocol string
	Host     string
	Banner   string
}

func ScanLocalhost(ports []uint16) ([]ServiceTarget, error) {
	targets := make([]plugins.Target, 0, len(ports))
	for _, p := range ports {
		addr, err := netip.ParseAddrPort(fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			return nil, fmt.Errorf("invalid port %d: %w", p, err)
		}
		targets = append(targets, plugins.Target{
			Address: addr,
			Host:    "127.0.0.1",
		})
	}

	cfg := scan.Config{
		DefaultTimeout: 2 * time.Second,
		FastMode:       true,
		Verbose:        false,
	}

	results, err := scan.ScanTargets(targets, cfg)
	if err != nil {
		return nil, err
	}

	out := make([]ServiceTarget, 0, len(results))
	for _, r := range results {
		banner := r.Version
		if banner == "" && r.Raw != nil {
			banner = string(r.Raw)
		}
		out = append(out, ServiceTarget{
			Port:     uint16(r.Port),
			Protocol: r.Protocol,
			Host:     "127.0.0.1",
			Banner:   banner,
		})
	}
	return out, nil
}
