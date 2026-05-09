package agent_test

import (
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/agent/agent"
	"git.konoss.org/kore/schmutz/agent/root"
)

func TestNewAgent_requiresEnabledRoot(t *testing.T) {
	dir := t.TempDir()
	r, _ := root.LoadRoot(dir) // no identity.json → not enabled
	_, err := agent.NewAgent(agent.DefaultConfig(), r)
	if err == nil {
		t.Fatal("expected error when root not enabled")
	}
}

func TestAgentConfig_defaults(t *testing.T) {
	cfg := agent.DefaultConfig()
	if cfg.RetryMinDelay == 0 {
		t.Error("RetryMinDelay should have a default")
	}
	if cfg.RetryMaxDelay == 0 {
		t.Error("RetryMaxDelay should have a default")
	}
}

func TestRetryCalculator(t *testing.T) {
	cfg := agent.DefaultConfig()
	calc := agent.NewRetryCalculator(cfg)
	d1 := calc.NextRetryDelay(1)
	d2 := calc.NextRetryDelay(2)
	if d2 <= d1 {
		t.Errorf("expected backoff to increase: d1=%v d2=%v", d1, d2)
	}
	_ = time.Second
}
