// Package telemetry dials telemetry.tango and streams typed frames to the controller.
package telemetry

import (
	"log"
	"net"
	"time"

	"git.konoss.org/kore/schmutz/agent/internal/collector"
	"git.konoss.org/kore/schmutz/agent/internal/stream"
	"github.com/openziti/sdk-golang/ziti"
)

// Dialer connects to telemetry.tango and publishes telemetry frames on a fixed interval.
// It reconnects automatically with exponential backoff on any failure.
type Dialer struct {
	identityPath string
	interval     time.Duration
	stop         chan struct{}
}

// NewDialer creates a Dialer using the given Ziti identity file.
func NewDialer(identityPath string, interval time.Duration) *Dialer {
	return &Dialer{
		identityPath: identityPath,
		interval:     interval,
		stop:         make(chan struct{}),
	}
}

// Run starts the dial-and-send loop. Blocks until Stop() is called.
func (d *Dialer) Run() {
	backoff := 5 * time.Second
	const maxBackoff = 5 * time.Minute

	for {
		select {
		case <-d.stop:
			return
		default:
		}

		if err := d.runOnce(); err != nil {
			log.Printf("telemetry: disconnected (%v) — retry in %s", err, backoff)
			select {
			case <-d.stop:
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = 5 * time.Second
	}
}

// Stop signals the dialer to exit cleanly.
func (d *Dialer) Stop() {
	close(d.stop)
}

func (d *Dialer) runOnce() error {
	cfg, err := ziti.NewConfigFromFile(d.identityPath)
	if err != nil {
		return err
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		return err
	}
	defer ctx.Close()

	conn, err := ctx.Dial("telemetry.tango")
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("telemetry: connected to telemetry.tango")

	enc, err := stream.NewEncoder(conn)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// Send an immediate heartbeat on connect so the controller knows we're alive.
	if err := enc.Send(stream.MsgHeartbeat, nil); err != nil {
		return err
	}

	for {
		select {
		case <-d.stop:
			return nil
		case <-ticker.C:
			if err := d.sendSnapshot(enc, conn); err != nil {
				return err
			}
		}
	}
}

func (d *Dialer) sendSnapshot(enc *stream.Encoder, conn net.Conn) error {
	snap := collector.CollectAll()

	if snap.System != nil {
		if err := enc.Send(stream.MsgSystem, snap.System); err != nil {
			return err
		}
	}
	if snap.Network != nil {
		if err := enc.Send(stream.MsgNetwork, snap.Network); err != nil {
			return err
		}
	}
	if snap.Disk != nil {
		if err := enc.Send(stream.MsgDisk, snap.Disk); err != nil {
			return err
		}
	}
	if snap.Process != nil {
		if err := enc.Send(stream.MsgProcess, snap.Process); err != nil {
			return err
		}
	}

	return nil
}
