package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Identity string       `yaml:"identity"`
	Listen   string       `yaml:"listen"`
	Log      LogConfig    `yaml:"log"`
	Store    StoreConfig  `yaml:"store"`
	Node     NodeConfig   `yaml:"node"`
	Limits   Limits       `yaml:"limits"`
	Health   HealthConfig `yaml:"health"`
	Rules    []Rule       `yaml:"rules"`
}

type HealthConfig struct {
	MaxHP         float64 `yaml:"max_hp"`
	RegenRate     float64 `yaml:"regen_rate"`
	RouteReward   float64 `yaml:"route_reward"`
	DropCost      float64 `yaml:"drop_cost"`
	DialFailCost  float64 `yaml:"dial_fail_cost"`
	BadHelloCost  float64 `yaml:"bad_hello_cost"`
	RateLimitCost float64 `yaml:"rate_limit_cost"`
	FloodCost     float64 `yaml:"flood_cost"`
	PersistSec    int     `yaml:"persist_sec"`
}

type StoreConfig struct {
	Path string `yaml:"path"` // BoltDB file path
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}

type NodeConfig struct {
	Name   string `yaml:"name"`
	Region string `yaml:"region"`
}

type Limits struct {
	MaxConnections int           `yaml:"max_connections"`
	PerSourceMax   int           `yaml:"per_source_max"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
}

type Rule struct {
	Name    string   `yaml:"name"`
	Comment string   `yaml:"comment"`
	SNI     *string  `yaml:"sni,omitempty"`
	JA4     []string `yaml:"ja4,omitempty"`
	JA4Not  []string `yaml:"ja4_not,omitempty"`
	SrcCIDR []string `yaml:"src_cidr,omitempty"`
	Service string   `yaml:"service,omitempty"`
	Action  string   `yaml:"action,omitempty"` // "route" (default) or "drop"
	Rate    string   `yaml:"rate,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Listen: ":443",
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Store: StoreConfig{
			Path: "/opt/schmutz/edge-gateway/edge-gateway.db",
		},
		Health: HealthConfig{
			MaxHP:         1000,
			RegenRate:     1.0,
			RouteReward:   0.5,
			DropCost:      2.0,
			DialFailCost:  1.0,
			BadHelloCost:  5.0,
			RateLimitCost: 3.0,
			FloodCost:     0.5,
			PersistSec:    10,
		},
		Limits: Limits{
			MaxConnections: 10000,
			PerSourceMax:   100,
			ReadTimeout:    10 * time.Second,
			IdleTimeout:    300 * time.Second,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Identity == "" {
		return nil, fmt.Errorf("identity path is required")
	}

	if len(cfg.Rules) == 0 {
		return nil, fmt.Errorf("at least one rule is required")
	}

	// Default action to "route" where service is set
	for i := range cfg.Rules {
		if cfg.Rules[i].Action == "" {
			if cfg.Rules[i].Service != "" {
				cfg.Rules[i].Action = "route"
			}
		}
	}

	return cfg, nil
}
