package classifier

import (
	"fmt"
	"net"

	"github.com/KontangoOSS/schmutz/internal/config"
)

// Result holds the classification decision for a connection.
type Result struct {
	Rule       string // Name of the matched rule
	Action     string // "route" or "drop"
	Service    string // Ziti service name (empty if dropped)
	RateWindow int    // Rate limit window in seconds (0 = no limit)
	RateMax    int    // Max connections per window (0 = no limit)
}

// Classifier evaluates rules against connection metadata.
type Classifier struct {
	rules []config.Rule
}

// New creates a classifier from a list of rules.
func New(rules []config.Rule) *Classifier {
	return &Classifier{rules: rules}
}

// Classify evaluates the connection metadata against all rules in order.
// Returns the first matching rule's result, or a default drop if no rules match.
func (c *Classifier) Classify(sni, ja4 string, srcIP net.IP) Result {
	for _, rule := range c.rules {
		if Match(&rule, sni, ja4, srcIP) {
			action := rule.Action
			if action == "" {
				action = "route"
			}
			rateWindow, rateMax := parseRate(rule.Rate)
			return Result{
				Rule:       rule.Name,
				Action:     action,
				Service:    rule.Service,
				RateWindow: rateWindow,
				RateMax:    rateMax,
			}
		}
	}

	// No rule matched — drop by default (deny-all)
	return Result{
		Rule:   "_default_deny",
		Action: "drop",
	}
}

// parseRate parses a rate string like "100/m" or "1000/h" into window seconds and max count.
func parseRate(rate string) (windowSec, maxCount int) {
	if rate == "" {
		return 0, 0
	}
	var count int
	var unit string
	if _, err := fmt.Sscanf(rate, "%d/%s", &count, &unit); err != nil {
		return 0, 0
	}
	switch unit {
	case "s":
		return 1, count
	case "m":
		return 60, count
	case "h":
		return 3600, count
	default:
		return 0, 0
	}
}
