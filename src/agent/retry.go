package agent

import (
	"math"
	"sync"
	"time"
)

type retryCalculator struct {
	cfg *AgentConfig
}

func newRetryCalculator(cfg *AgentConfig) *retryCalculator {
	return &retryCalculator{cfg: cfg}
}

// NewRetryCalculator is the exported alias for tests.
func NewRetryCalculator(cfg *AgentConfig) *retryCalculator {
	return newRetryCalculator(cfg)
}

func (rc *retryCalculator) nextRetryTime(count int) time.Time {
	return time.Now().Add(rc.NextRetryDelay(count))
}

// NextRetryDelay returns exponential backoff delay capped at RetryMaxDelay.
func (rc *retryCalculator) NextRetryDelay(count int) time.Duration {
	delay := float64(rc.cfg.RetryMinDelay) * math.Pow(2, float64(count-1))
	max := float64(rc.cfg.RetryMaxDelay)
	if delay > max {
		delay = max
	}
	return time.Duration(delay) * time.Second
}

type retryManager struct {
	agent          *Agent
	mu             sync.Mutex
	failedServices []*serviceRegistryEntry
	stopCh         chan struct{}
}

func newRetryManager(a *Agent) *retryManager {
	return &retryManager{agent: a, stopCh: make(chan struct{})}
}

func (rm *retryManager) addFailedService(entry *serviceRegistryEntry) {
	rm.mu.Lock()
	rm.failedServices = append(rm.failedServices, entry)
	rm.mu.Unlock()
}

func (rm *retryManager) run() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rm.mu.Lock()
			now := time.Now()
			var remaining []*serviceRegistryEntry
			for _, entry := range rm.failedServices {
				if entry.Failure == nil || now.After(entry.Failure.NextRetry) {
					if err := rm.agent.StartService(entry.Request); err != nil {
						entry.Failure.Count++
						entry.Failure.LastError = err.Error()
						entry.Failure.NextRetry = rm.agent.retryCalc.nextRetryTime(entry.Failure.Count)
						remaining = append(remaining, entry)
					}
				} else {
					remaining = append(remaining, entry)
				}
			}
			rm.failedServices = remaining
			rm.mu.Unlock()
		case <-rm.stopCh:
			return
		}
	}
}

func (rm *retryManager) stop() {
	close(rm.stopCh)
}
