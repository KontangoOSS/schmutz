package service

import (
	"encoding/json"
	"io"
	"log"
	"sync"
	"time"

	"github.com/openziti/sdk-golang/ziti/edge"
)

// RelayFrame is the wire format sent to relay consumers (e.g. ziti-dash).
// One object per line (newline-delimited JSON).
type RelayFrame struct {
	MachineID string          `json:"machine_id"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"ts"`
	Payload   json.RawMessage `json:"payload"`
}

// RelayService fans tagged telemetry frames to relay.tango dial connections.
// ziti-dash (or any trusted consumer) dials relay.tango and receives a stream
// of newline-delimited JSON RelayFrame objects.
type RelayService struct {
	listener edge.Listener
	mu       sync.RWMutex
	clients  map[chan RelayFrame]struct{}
	stop     chan struct{}
}

// NewRelayService creates a relay bound to ln (a Ziti edge listener).
func NewRelayService(ln edge.Listener) *RelayService {
	return &RelayService{
		listener: ln,
		clients:  make(map[chan RelayFrame]struct{}),
		stop:     make(chan struct{}),
	}
}

// Start begins accepting relay consumer connections in the background.
func (s *RelayService) Start() {
	go s.acceptLoop()
	log.Printf("relay: listening on relay.tango")
}

// Stop shuts down the relay.
func (s *RelayService) Stop() {
	close(s.stop)
	s.listener.Close()
}

// Publish sends a frame to all connected relay consumers.
func (s *RelayService) Publish(f RelayFrame) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- f:
		default: // drop on slow consumer — never block
		}
	}
}

func (s *RelayService) acceptLoop() {
	for {
		conn, err := s.listener.AcceptEdge()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				log.Printf("relay: accept: %v", err)
				return
			}
		}
		go s.handleRelay(conn)
	}
}

func (s *RelayService) handleRelay(conn edge.Conn) {
	defer conn.Close()

	consumer := conn.GetDialerIdentityName()
	log.Printf("relay: consumer connected: %s", consumer)

	ch := make(chan RelayFrame, 256)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
		close(ch)
	}()

	enc := json.NewEncoder(conn)
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	// keepaliveFrame is a minimal JSON comment-style frame ziti-dash ignores.
	keepaliveFrame := RelayFrame{Type: "keepalive"}

	for {
		select {
		case <-s.stop:
			return
		case <-keepalive.C:
			// Write a keepalive to prevent Ziti idle connection timeout.
			if err := enc.Encode(keepaliveFrame); err != nil {
				return
			}
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(frame); err != nil {
				if err != io.EOF {
					log.Printf("relay: %s: write: %v", consumer, err)
				}
				return
			}
		}
	}
}
