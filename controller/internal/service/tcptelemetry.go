package service

import (
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/stream"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// TCPTelemetryService accepts telemetry connections on a Ziti edge listener
// and fans frames into the in-process TelemetryService.
type TCPTelemetryService struct {
	listener edge.Listener
	tel      *TelemetryService
	relay    *RelayService  // set after construction via SetRelay
	stop     chan struct{}
}

// NewTCPTelemetryService creates a service that reads from ln and dispatches to tel.
func NewTCPTelemetryService(ln edge.Listener, tel *TelemetryService) *TCPTelemetryService {
	return &TCPTelemetryService{
		listener: ln,
		tel:      tel,
		stop:     make(chan struct{}),
	}
}

// SetRelay wires a RelayService to receive all tagged frames.
func (s *TCPTelemetryService) SetRelay(r *RelayService) {
	s.relay = r
}

// Start begins accepting connections in background goroutines.
func (s *TCPTelemetryService) Start() {
	go s.acceptLoop()
	log.Printf("tcptelemetry: listening on telemetry.tango")
}

// Stop closes the listener and signals all goroutines to exit.
func (s *TCPTelemetryService) Stop() {
	close(s.stop)
	s.listener.Close()
}

func (s *TCPTelemetryService) acceptLoop() {
	for {
		conn, err := s.listener.AcceptEdge()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				log.Printf("tcptelemetry: accept: %v", err)
				return
			}
		}
		go s.handleConn(conn)
	}
}

func (s *TCPTelemetryService) handleConn(conn edge.Conn) {
	defer conn.Close()

	machineID := conn.GetDialerIdentityName()
	log.Printf("tcptelemetry: connected: %s", machineID)

	dec, err := stream.NewDecoder(conn)
	if err != nil {
		log.Printf("tcptelemetry: decoder: %v", err)
		return
	}

	for {
		select {
		case <-s.stop:
			return
		default:
		}

		msg, err := dec.Receive()
		if err != nil {
			if err != io.EOF {
				log.Printf("tcptelemetry: %s: read: %v", machineID, err)
			}
			return
		}

		s.dispatch(machineID, msg)
	}
}

func (s *TCPTelemetryService) dispatch(machineID string, msg *stream.Message) {
	slug := stream.MsgName(msg.Type)

	// Normalize payload — empty frames (e.g. heartbeat) use null JSON.
	payload := json.RawMessage(msg.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}

	ev := &Event{
		MachineID: machineID,
		Type:      slug,
		Timestamp: msg.Timestamp.Unix(),
		Data:      payload,
	}
	s.tel.RecordEvent(ev)

	frame := RelayFrame{
		MachineID: machineID,
		Type:      slug,
		Timestamp: msg.Timestamp.Unix(),
		Payload:   payload,
	}
	if s.relay != nil {
		s.relay.Publish(frame)
	}
}

// RecordSelf allows the selfmonitor to publish without a network socket.
func (s *TCPTelemetryService) RecordSelf(machineID, slug string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	ev := &Event{
		MachineID: machineID,
		Type:      slug,
		Data:      json.RawMessage(payload),
	}
	s.tel.RecordEvent(ev)

	selfFrame := RelayFrame{
		MachineID: machineID,
		Type:      slug,
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(payload),
	}
	if s.relay != nil {
		s.relay.Publish(selfFrame)
	}
}

