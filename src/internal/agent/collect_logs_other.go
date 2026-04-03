//go:build !linux

package agent

// logCollector is a no-op on non-Linux platforms.
type logCollector struct{}

func (c *logCollector) collect(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte) {
	<-ctx.Done()
}
