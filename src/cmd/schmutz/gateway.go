package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openziti/sdk-golang/ziti"

	"github.com/KontangoOSS/schmutz/internal/classifier"
	"github.com/KontangoOSS/schmutz/internal/clienthello"
	"github.com/KontangoOSS/schmutz/internal/config"
	"github.com/KontangoOSS/schmutz/internal/health"
	"github.com/KontangoOSS/schmutz/internal/relay"
	"github.com/KontangoOSS/schmutz/internal/store"
)

func startGateway(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	var handler slog.Handler
	logWriter := os.Stdout
	if cfg.Log.File != "" {
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		logWriter = f
	}

	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Log.Format == "text" {
		handler = slog.NewTextHandler(logWriter, opts)
	} else {
		handler = slog.NewJSONHandler(logWriter, opts)
	}
	logger := slog.New(handler).With(
		"edge_node", cfg.Node.Name,
		"region", cfg.Node.Region,
	)

	db, err := store.Open(cfg.Store.Path)
	if err != nil {
		logger.Error("open store", "path", cfg.Store.Path, "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("store opened", "path", cfg.Store.Path)

	hpCfg := health.Config{
		MaxHP:         cfg.Health.MaxHP,
		RegenRate:     cfg.Health.RegenRate,
		RouteReward:   cfg.Health.RouteReward,
		DropCost:      cfg.Health.DropCost,
		DialFailCost:  cfg.Health.DialFailCost,
		BadHelloCost:  cfg.Health.BadHelloCost,
		RateLimitCost: cfg.Health.RateLimitCost,
		FloodCost:     cfg.Health.FloodCost,
		PersistSec:    cfg.Health.PersistSec,
	}
	hp, err := health.NewPool(db.DB(), hpCfg)
	if err != nil {
		logger.Error("init health pool", "error", err)
		os.Exit(1)
	}
	defer hp.Stop()
	logger.Info("health pool ready", "hp", hp.HP(), "level", hp.Level().String())

	zitiCtx, err := ziti.NewContextFromFile(cfg.Identity)
	if err != nil {
		logger.Error("load ziti identity", "identity", cfg.Identity, "error", err)
		os.Exit(1)
	}
	defer zitiCtx.Close()
	logger.Info("ziti context ready", "identity", cfg.Identity)

	cls := classifier.New(cfg.Rules)

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Error("listen", "address", cfg.Listen, "error", err)
		os.Exit(1)
	}
	defer ln.Close()

	logger.Info("listening",
		"address", cfg.Listen,
		"rules", len(cfg.Rules),
		"max_connections", cfg.Limits.MaxConnections,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())
		cancel()
		ln.Close()
	}()

	var activeConns atomic.Int64

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			logger.Error("accept", "error", err)
			continue
		}

		current := activeConns.Add(1)
		if int(current) > cfg.Limits.MaxConnections {
			activeConns.Add(-1)
			conn.Close()
			db.IncrStat("conn_rejected_limit")
			logger.Warn("connection limit reached", "limit", cfg.Limits.MaxConnections)
			continue
		}

		go func() {
			defer activeConns.Add(-1)
			handleConnection(conn, cls, zitiCtx, db, hp, cfg, logger)
		}()
	}

	logger.Info("draining connections", "active", activeConns.Load())
	deadline := time.After(30 * time.Second)
	for activeConns.Load() > 0 {
		select {
		case <-deadline:
			logger.Warn("drain timeout", "remaining", activeConns.Load())
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	logger.Info("shutdown complete")
}

func handleConnection(conn net.Conn, cls *classifier.Classifier, zitiCtx ziti.Context, db *store.Store, hp *health.Pool, cfg *config.Config, logger *slog.Logger) {
	defer conn.Close()
	start := time.Now()

	srcAddr := conn.RemoteAddr().(*net.TCPAddr)
	srcIP := srcAddr.IP
	level := hp.Level()

	db.IncrStat("conn_total")

	info, replayConn, err := clienthello.Peek(conn, cfg.Limits.ReadTimeout)
	if err != nil {
		db.IncrStat("conn_bad_clienthello")
		hp.RecordBadHello()
		logger.Debug("bad clienthello", "src", srcAddr.String(), "error", err, "hp", hp.HP(), "level", level.String())
		return
	}

	ja4 := clienthello.JA4(info)
	result := cls.Classify(info.SNI, ja4, srcIP)

	db.RecordJA4(ja4, info.SNI, srcAddr.String(), result.Action)
	db.RecordSNI(info.SNI, ja4, srcAddr.String())

	connLogger := logger.With(
		"src", srcAddr.String(),
		"sni", info.SNI,
		"ja4", ja4,
		"rule", result.Rule,
		"hp", int(hp.HP()),
		"level", level.String(),
	)

	if result.Action != "drop" && hp.ShouldDropUnknown() {
		// TODO: check JA4 against known-good list
	}

	if result.Action != "drop" && result.Rule == "catch-all" && hp.ShouldDropCatchAll() {
		result.Action = "drop"
		result.Rule = "hp-red-catchall-shed"
		connLogger = connLogger.With("rule", result.Rule)
	}

	if result.Action == "drop" {
		db.IncrStat("conn_dropped")
		hp.RecordDrop()
		connLogger.Info("dropped")
		return
	}

	if result.RateWindow > 0 && result.RateMax > 0 {
		effectiveMax := int(float64(result.RateMax) * hp.RateLimitMultiplier())
		if effectiveMax < 1 {
			effectiveMax = 1
		}
		allowed, err := db.CheckRateLimit(srcIP.String(), result.RateWindow, effectiveMax)
		if err != nil {
			connLogger.Error("rate limit check", "error", err)
		}
		if !allowed {
			db.IncrStat("conn_rate_limited")
			hp.RecordRateLimit()
			connLogger.Info("rate limited", "service", result.Service)
			return
		}
	}

	zitiConn, err := zitiCtx.Dial(result.Service)
	if err != nil {
		db.IncrStat("conn_dial_failed")
		hp.RecordDialFail()
		connLogger.Error("dial failed", "service", result.Service, "error", err)
		return
	}
	defer zitiConn.Close()

	db.IncrStat("conn_routed")
	hp.RecordRoute()

	bytesIn, bytesOut := relay.Bidirectional(replayConn, zitiConn)

	connLogger.Info("completed",
		"service", result.Service,
		"duration_ms", time.Since(start).Milliseconds(),
		"bytes_in", bytesIn,
		"bytes_out", bytesOut,
	)
}
