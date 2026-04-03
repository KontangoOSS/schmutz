//go:build linux

package agent

import (
	"bufio"
	"os"
	"strings"
)

// readARP parses /proc/net/arp on Linux.
// Format: IP address HW type Flags HW address Mask Device
func readARP() []ARPEntry {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []ARPEntry
	sc := bufio.NewScanner(f)
	sc.Scan() // skip header line
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 6 {
			continue
		}
		ip := fields[0]
		mac := fields[3]
		dev := fields[5]
		// Skip incomplete entries (00:00:00:00:00:00)
		if mac == "00:00:00:00:00:00" {
			continue
		}
		entries = append(entries, ARPEntry{IP: ip, MAC: mac, Dev: dev})
	}
	return entries
}
