package agent

import "time"

// heartbeatCollector emits a heartbeat event on every interval tick.
// interval is driven by the intervalCh — resets whenever the controller
// pushes a new value.
type heartbeatCollector struct {
	intervalCh <-chan time.Duration
	initial    time.Duration
}

func (c *heartbeatCollector) collect(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte) {
	interval := c.initial
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	emit := func() {
		hb := buildHeartbeat(machineID)
		if b, err := encodeEvent(machineID, "hb", hb); err == nil {
			select {
			case out <- b:
			default:
			}
		}
	}

	emit()
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-c.intervalCh:
			interval = d
			ticker.Reset(d)
		case <-ticker.C:
			emit()
		}
	}
}
