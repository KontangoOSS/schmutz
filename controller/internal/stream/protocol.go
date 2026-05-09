// Package stream defines the telemetry wire protocol for schmutz agents.
package stream

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Message types
const (
	MsgHeartbeat uint8 = 0x01
	MsgSystem    uint8 = 0x02
	MsgNetwork   uint8 = 0x03
	MsgProcess   uint8 = 0x04
	MsgDisk      uint8 = 0x05
	MsgService   uint8 = 0x06
	MsgLog       uint8 = 0x07
	MsgEvent     uint8 = 0x08
	MsgCustom    uint8 = 0xFF
)

// MsgName returns a human-readable name for a message type.
func MsgName(t uint8) string {
	switch t {
	case MsgHeartbeat:
		return "heartbeat"
	case MsgSystem:
		return "system"
	case MsgNetwork:
		return "network"
	case MsgProcess:
		return "process"
	case MsgDisk:
		return "disk"
	case MsgService:
		return "service"
	case MsgLog:
		return "log"
	case MsgEvent:
		return "event"
	case MsgCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// Frame is the wire format header.
// [1 byte type][4 bytes payload len][8 bytes timestamp nanos]
const HeaderSize = 1 + 4 + 8 // 13 bytes

// SystemData is the payload for MsgSystem.
type SystemData struct {
	Hostname   string    `json:"hostname"`
	CPUPercent float64   `json:"cpu_percent"`
	MemTotal   uint64    `json:"mem_total"`
	MemUsed    uint64    `json:"mem_used"`
	MemPercent float64   `json:"mem_percent"`
	LoadAvg1   float64   `json:"load_avg_1"`
	LoadAvg5   float64   `json:"load_avg_5"`
	LoadAvg15  float64   `json:"load_avg_15"`
	Uptime     uint64    `json:"uptime"`
	NumCPU     int       `json:"num_cpu"`
}

// NetworkData is the payload for MsgNetwork.
type NetworkData struct {
	Interfaces []NetInterface `json:"interfaces"`
}

type NetInterface struct {
	Name      string `json:"name"`
	BytesSent uint64 `json:"bytes_sent"`
	BytesRecv uint64 `json:"bytes_recv"`
	IP        string `json:"ip,omitempty"`
}

// ProcessData is the payload for MsgProcess.
type ProcessData struct {
	Total     int      `json:"total"`
	Running   int      `json:"running"`
	TopCPU    []string `json:"top_cpu,omitempty"`
	TopMem    []string `json:"top_mem,omitempty"`
}

// DiskData is the payload for MsgDisk.
type DiskData struct {
	Mounts []DiskMount `json:"mounts"`
}

type DiskMount struct {
	Path        string  `json:"path"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
	Fstype      string  `json:"fstype"`
}

// EventData is the payload for MsgEvent.
type EventData struct {
	Type    string `json:"type"` // "enrolled", "promoted", "error", etc.
	Message string `json:"message"`
}

// Encoder writes typed, compressed messages to a writer.
type Encoder struct {
	w   io.Writer
	enc *zstd.Encoder
}

// NewEncoder creates a stream encoder.
func NewEncoder(w io.Writer) (*Encoder, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, err
	}
	return &Encoder{w: w, enc: enc}, nil
}

// Send writes a typed message to the stream.
func (e *Encoder) Send(msgType uint8, payload interface{}) error {
	// Marshal payload to JSON (heartbeat has no payload)
	var jsonData []byte
	if payload != nil {
		var err error
		jsonData, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	// Compress
	compressed := e.enc.EncodeAll(jsonData, nil)

	// Write header: [type][length][timestamp]
	header := make([]byte, HeaderSize)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:5], uint32(len(compressed)))
	binary.BigEndian.PutUint64(header[5:13], uint64(time.Now().UnixNano()))

	if _, err := e.w.Write(header); err != nil {
		return err
	}
	if _, err := e.w.Write(compressed); err != nil {
		return err
	}
	return nil
}

// Decoder reads typed, compressed messages from a reader.
type Decoder struct {
	r   io.Reader
	dec *zstd.Decoder
}

// NewDecoder creates a stream decoder.
func NewDecoder(r io.Reader) (*Decoder, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	return &Decoder{r: r, dec: dec}, nil
}

// Message is a decoded wire message.
type Message struct {
	Type      uint8
	Timestamp time.Time
	Payload   []byte // decompressed JSON
}

// Receive reads the next message from the stream.
func (d *Decoder) Receive() (*Message, error) {
	// Read header
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(d.r, header); err != nil {
		return nil, err
	}

	msgType := header[0]
	payloadLen := binary.BigEndian.Uint32(header[1:5])
	tsNanos := binary.BigEndian.Uint64(header[5:13])

	// Read compressed payload
	compressed := make([]byte, payloadLen)
	if _, err := io.ReadFull(d.r, compressed); err != nil {
		return nil, err
	}

	// Decompress
	decompressed, err := d.dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Unix(0, int64(tsNanos)),
		Payload:   decompressed,
	}, nil
}
