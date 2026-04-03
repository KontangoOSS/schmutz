package agent

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"time"
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
