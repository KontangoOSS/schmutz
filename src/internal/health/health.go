package health

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketHealth = []byte("health")

// Level represents the node's current defensive posture.
type Level int

const (
	Green  Level = iota // HP > 75%: normal operation, all rules as configured
	Yellow              // HP 50-75%: tightened rate limits, unknown JA4 scrutiny
	Orange              // HP 25-50%: only known-good traffic, unknowns dropped
	Red                 // HP 0-25%: survival mode, only allowlisted SNIs pass
)

func (l Level) String() string {
	switch l {
	case Green:
		return "green"
	case Yellow:
		return "yellow"
	case Orange:
		return "orange"
	case Red:
		return "red"
	default:
		return "unknown"
	}
}

// Config controls the HP system behavior.
type Config struct {
	MaxHP         float64 `yaml:"max_hp"`          // Starting/max HP (default 1000)
	RegenRate     float64 `yaml:"regen_rate"`      // HP recovered per second passively (default 1.0)
	RouteReward   float64 `yaml:"route_reward"`    // HP gained per successful route (default 0.5)
	DropCost      float64 `yaml:"drop_cost"`       // HP lost per dropped connection (default 2.0)
	DialFailCost  float64 `yaml:"dial_fail_cost"`  // HP lost per failed dial (default 1.0)
	BadHelloCost  float64 `yaml:"bad_hello_cost"`  // HP lost per malformed ClientHello (default 5.0)
	RateLimitCost float64 `yaml:"rate_limit_cost"` // HP lost per rate-limited connection (default 3.0)
	FloodCost     float64 `yaml:"flood_cost"`      // HP lost per connection when under Yellow+ (scales with level)
	PersistSec    int     `yaml:"persist_sec"`     // How often to persist HP to BoltDB (default 10)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxHP:         1000,
		RegenRate:     1.0,
		RouteReward:   0.5,
		DropCost:      2.0,
		DialFailCost:  1.0,
		BadHelloCost:  5.0,
		RateLimitCost: 3.0,
		FloodCost:     0.5,
		PersistSec:    10,
	}
}

// Pool tracks the node's health points.
type Pool struct {
	mu       sync.RWMutex
	hp       float64
	cfg      Config
	db       *bolt.DB
	lastTick time.Time
	stopCh   chan struct{}
}

// NewPool creates an HP pool, restoring from BoltDB if available.
func NewPool(db *bolt.DB, cfg Config) (*Pool, error) {
	// Ensure bucket exists
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketHealth)
		return err
	})
	if err != nil {
		return nil, err
	}

	p := &Pool{
		hp:       cfg.MaxHP,
		cfg:      cfg,
		db:       db,
		lastTick: time.Now(),
		stopCh:   make(chan struct{}),
	}

	// Restore HP from BoltDB
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketHealth)
		if data := b.Get([]byte("hp")); data != nil && len(data) == 8 {
			bits := binary.BigEndian.Uint64(data)
			restored := math.Float64frombits(bits)
			if restored > 0 && restored <= cfg.MaxHP {
				p.hp = restored
			}
		}
		return nil
	})

	// Start passive regen + persist ticker
	go p.tick()

	return p, nil
}

// Stop cleanly shuts down the HP pool and persists final state.
func (p *Pool) Stop() {
	close(p.stopCh)
	p.persist()
}

// HP returns the current health points.
func (p *Pool) HP() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.hp
}

// Percent returns HP as a percentage of max.
func (p *Pool) Percent() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return (p.hp / p.cfg.MaxHP) * 100
}

// Level returns the current defensive posture.
func (p *Pool) Level() Level {
	pct := p.Percent()
	switch {
	case pct > 75:
		return Green
	case pct > 50:
		return Yellow
	case pct > 25:
		return Orange
	default:
		return Red
	}
}

// ConnectionCost returns the current cost of processing a connection.
// At Green, cost is near zero. At Red, cost is very high — the node
// "charges more" to let traffic through.
func (p *Pool) ConnectionCost() float64 {
	level := p.Level()
	switch level {
	case Green:
		return 0
	case Yellow:
		return p.cfg.FloodCost
	case Orange:
		return p.cfg.FloodCost * 3
	case Red:
		return p.cfg.FloodCost * 10
	default:
		return 0
	}
}

// RateLimitMultiplier returns how much to tighten rate limits.
// At Green = 1.0 (normal). At Red = 0.1 (10% of normal capacity).
func (p *Pool) RateLimitMultiplier() float64 {
	level := p.Level()
	switch level {
	case Green:
		return 1.0
	case Yellow:
		return 0.5
	case Orange:
		return 0.25
	case Red:
		return 0.1
	default:
		return 1.0
	}
}

// ShouldDropUnknown returns true if unknown JA4 fingerprints should be dropped.
func (p *Pool) ShouldDropUnknown() bool {
	return p.Level() >= Orange
}

// ShouldDropCatchAll returns true if catch-all traffic should be dropped.
func (p *Pool) ShouldDropCatchAll() bool {
	return p.Level() >= Red
}

// RecordRoute — a connection was successfully routed. Heals the node.
func (p *Pool) RecordRoute() {
	p.mu.Lock()
	defer p.mu.Unlock()
	cost := p.connectionCostLocked()
	net := p.cfg.RouteReward - cost
	p.hp = clamp(p.hp+net, 0, p.cfg.MaxHP)
}

// RecordDrop — a connection was dropped by rules. Damages the node.
func (p *Pool) RecordDrop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hp = clamp(p.hp-p.cfg.DropCost, 0, p.cfg.MaxHP)
}

// RecordDialFail — a Ziti dial failed.
func (p *Pool) RecordDialFail() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hp = clamp(p.hp-p.cfg.DialFailCost, 0, p.cfg.MaxHP)
}

// RecordBadHello — malformed TLS ClientHello. Strong negative signal.
func (p *Pool) RecordBadHello() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hp = clamp(p.hp-p.cfg.BadHelloCost, 0, p.cfg.MaxHP)
}

// RecordRateLimit — connection was rate limited.
func (p *Pool) RecordRateLimit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hp = clamp(p.hp-p.cfg.RateLimitCost, 0, p.cfg.MaxHP)
}

func (p *Pool) connectionCostLocked() float64 {
	pct := (p.hp / p.cfg.MaxHP) * 100
	switch {
	case pct > 75:
		return 0
	case pct > 50:
		return p.cfg.FloodCost
	case pct > 25:
		return p.cfg.FloodCost * 3
	default:
		return p.cfg.FloodCost * 10
	}
}

// tick runs passive HP regeneration and periodic BoltDB persistence.
func (p *Pool) tick() {
	ticker := time.NewTicker(time.Duration(p.cfg.PersistSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case now := <-ticker.C:
			p.mu.Lock()
			elapsed := now.Sub(p.lastTick).Seconds()
			p.hp = clamp(p.hp+(p.cfg.RegenRate*elapsed), 0, p.cfg.MaxHP)
			p.lastTick = now
			p.mu.Unlock()
			p.persist()
		}
	}
}

func (p *Pool) persist() {
	p.mu.RLock()
	hp := p.hp
	p.mu.RUnlock()

	p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketHealth)
		data := make([]byte, 8)
		binary.BigEndian.PutUint64(data, math.Float64bits(hp))
		return b.Put([]byte("hp"), data)
	})
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
