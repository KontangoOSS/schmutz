//go:build !linux

package agent

func readARP() []ARPEntry { return nil }
