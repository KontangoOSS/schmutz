package agent

import (
	"net"
	"time"
)

// NetSample is a compact snapshot of local network state.
// Short field names minimise JSON size before compression.
type NetSample struct {
	Ifaces []NetIface `json:"i,omitempty"`
	ARP    []ARPEntry `json:"a,omitempty"`
}

// NetIface is one network interface.
type NetIface struct {
	N    string   `json:"n"`           // name
	MAC  string   `json:"m,omitempty"` // hardware address
	Addr []string `json:"a,omitempty"` // IP/prefix list
	Up   bool     `json:"u"`           // FlagUp
	Lo   bool     `json:"l,omitempty"` // FlagLoopback
}

// ARPEntry is one row from the ARP cache.
type ARPEntry struct {
	IP  string `json:"i"`           // IP address
	MAC string `json:"m"`           // hardware address
	Dev string `json:"d,omitempty"` // interface name
}

// netCollector samples network interfaces and ARP at a fixed interval.
type netCollector struct {
	interval time.Duration
}

func (c *netCollector) collect(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte) {
	iv := c.interval
	if iv <= 0 {
		iv = 5 * time.Minute
	}

	emit := func() {
		s := buildNetSample()
		if b, err := encodeEvent(machineID, "net", s); err == nil {
			select {
			case out <- b:
			default:
			}
		}
	}

	emit()
	ticker := time.NewTicker(iv)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			emit()
		}
	}
}

func buildNetSample() NetSample {
	ifaces, _ := net.Interfaces()
	s := NetSample{
		Ifaces: make([]NetIface, 0, len(ifaces)),
	}
	for _, iface := range ifaces {
		ni := NetIface{
			N:   iface.Name,
			MAC: iface.HardwareAddr.String(),
			Up:  iface.Flags&net.FlagUp != 0,
			Lo:  iface.Flags&net.FlagLoopback != 0,
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ni.Addr = append(ni.Addr, a.String())
		}
		s.Ifaces = append(s.Ifaces, ni)
	}
	s.ARP = readARP()
	return s
}
