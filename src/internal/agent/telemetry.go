package agent

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"net"
	"time"

	syscollector "github.com/KontangoOSS/schmutz/internal/collector"
	"github.com/KontangoOSS/schmutz/internal/stream"
)

// Event is the universal envelope for every message on the telemetry channel.
// Wire format: zlib-compressed JSON {"m":"<mid>","t":"<type>","ts":<unix>,"d":{...}}
// Short keys keep the uncompressed payload small; zlib handles repetition.
type Event struct {
	MachineID string          `json:"m"`
	Type      string          `json:"t"`
	Timestamp int64           `json:"ts"`
	Data      json.RawMessage `json:"d"`
}

// encodeEvent serialises and zlib-compresses an event in one call.
// All collectors use this — same path, same wire format.
func encodeEvent(machineID, evType string, payload interface{}) ([]byte, error) {
	d, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	env, err := json.Marshal(Event{
		MachineID: machineID,
		Type:      evType,
		Timestamp: time.Now().Unix(),
		Data:      d,
	})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(env)
	w.Close()
	return buf.Bytes(), nil
}

// decodeEvent decompresses and deserialises an event from the wire.
func decodeEvent(b []byte) (*Event, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var ev Event
	if err := json.NewDecoder(r).Decode(&ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// collector sends compressed events to a channel as fast as it can.
// Each collector runs in its own goroutine and is started once.
type collector interface {
	// collect runs until ctx is done. It writes compressed Event bytes to out
	// as soon as each sample is ready. Use encodeEvent to produce bytes.
	collect(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte)
}

// StreamTelemetry sends periodic telemetry snapshots over a connection.
// It uses the stream.Encoder to compress and frame each message.
// Blocks until ctx is cancelled or the connection fails.
// The caller is responsible for providing the connection (Ziti dial or raw TCP).
func StreamTelemetry(ctx context.Context, conn net.Conn, interval time.Duration) error {
	enc, err := stream.NewEncoder(conn)
	if err != nil {
		return err
	}

	// Send initial connected event.
	if err := enc.Send(stream.MsgEvent, &stream.EventData{Type: "connected"}); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var tick int

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick++

			// System telemetry — every tick.
			if sys, err := syscollector.CollectSystem(); err == nil {
				if err := enc.Send(stream.MsgSystem, sys); err != nil {
					return err
				}
			}

			// Network telemetry — every tick.
			if net, err := syscollector.CollectNetwork(); err == nil {
				if err := enc.Send(stream.MsgNetwork, net); err != nil {
					return err
				}
			}

			// Disk + heartbeat — every 6th tick (~30s at 5s interval).
			if tick%6 == 0 {
				if disk, err := syscollector.CollectDisk(); err == nil {
					if err := enc.Send(stream.MsgDisk, disk); err != nil {
						return err
					}
				}
				if err := enc.Send(stream.MsgHeartbeat, nil); err != nil {
					return err
				}
			}

			// Processes — every 12th tick (~60s at 5s interval).
			if tick%12 == 0 {
				if procs, err := syscollector.CollectProcesses(); err == nil {
					if err := enc.Send(stream.MsgProcess, procs); err != nil {
						return err
					}
				}
			}
		}
	}
}
