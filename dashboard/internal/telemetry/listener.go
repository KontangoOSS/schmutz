package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"time"

	"github.com/openziti/sdk-golang/ziti"
)

// Listener dials relay.tango over Ziti and publishes received frames to Hub.
// relay.tango sends newline-delimited JSON: one Frame object per line.
// Reconnects automatically with exponential backoff.
type Listener struct {
	identityPath string
	hub          *Hub
	stop         chan struct{}
}

// NewListener creates a listener using the given Ziti identity file.
func NewListener(identityPath string, hub *Hub) *Listener {
	return &Listener{
		identityPath: identityPath,
		hub:          hub,
		stop:         make(chan struct{}),
	}
}

// Run starts the dial loop. Blocks until Stop() is called.
func (l *Listener) Run() {
	backoff := 10 * time.Second
	const maxBackoff = 5 * time.Minute

	for {
		select {
		case <-l.stop:
			return
		default:
		}

		if err := l.runOnce(); err != nil {
			log.Printf("telemetry listener: disconnected (%v) — retry in %s", err, backoff)
			select {
			case <-l.stop:
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
		backoff = 10 * time.Second
	}
}

// Stop signals the listener to exit.
func (l *Listener) Stop() {
	close(l.stop)
}

func (l *Listener) runOnce() (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("ziti SDK panic: %v\n%s", r, debug.Stack())
		}
	}()

	cfg, err := ziti.NewConfigFromFile(l.identityPath)
	if err != nil {
		return err
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		return err
	}
	defer ctx.Close()

	conn, err := ctx.Dial("relay.tango")
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("telemetry listener: connected to relay.tango")

	dec := json.NewDecoder(conn)
	for {
		select {
		case <-l.stop:
			return nil
		default:
		}

		var frame Frame
		if err := dec.Decode(&frame); err != nil {
			if err != io.EOF {
				log.Printf("telemetry listener: decode: %v", err)
			}
			return err
		}

		// Skip keepalive frames (no machine_id or type="keepalive")
		if frame.MachineID == "" || frame.Type == "keepalive" {
			continue
		}

		l.hub.Publish(frame)
	}
}
