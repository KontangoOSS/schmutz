package agent

const (
	// DefaultDomain is the default controller domain.
	DefaultDomain = "ctrl.konoss.org"

	// DefaultTelemetryService is the Ziti service for telemetry streaming.
	DefaultTelemetryService = "telemetry-stream.tango"

	// AgentVersion is the current agent version string.
	AgentVersion = "0.1.0"

	// UserAgent is the HTTP User-Agent header sent by the agent.
	UserAgent = "schmutz-agent/" + AgentVersion
)
