//go:build !linux

package agent

import "time"

// startTime is used for uptime approximation on non-Linux platforms.
var startTime = time.Now()

func uptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

func memoryMB() int64 {
	return 0
}

func loadAvg1() float64 {
	return 0
}
