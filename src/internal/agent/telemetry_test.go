package agent

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/internal/stream"
)

// readMessages reads messages from dec into msgs channel until the connection
// is closed or an error occurs.
func readMessages(dec *stream.Decoder, msgs chan<- *stream.Message) {
	for {
		msg, err := dec.Receive()
		if err != nil {
			close(msgs)
			return
		}
		msgs <- msg
	}
}

// TestStreamSendsConnectedEvent verifies that the very first message sent is a
// MsgEvent with Type "connected".
func TestStreamSendsConnectedEvent(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	dec, err := stream.NewDecoder(server)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	msgs := make(chan *stream.Message, 64)
	go readMessages(dec, msgs)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- StreamTelemetry(ctx, client, 500*time.Millisecond)
	}()

	// Wait for the first message.
	select {
	case msg, ok := <-msgs:
		if !ok {
			t.Fatal("message channel closed before receiving connected event")
		}
		if msg.Type != stream.MsgEvent {
			t.Fatalf("first message type = %d (%s), want MsgEvent (%d)", msg.Type, stream.MsgName(msg.Type), stream.MsgEvent)
		}
		var ev stream.EventData
		if err := json.Unmarshal(msg.Payload, &ev); err != nil {
			t.Fatalf("unmarshal EventData: %v", err)
		}
		if ev.Type != "connected" {
			t.Fatalf("event type = %q, want %q", ev.Type, "connected")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for connected event")
	}

	cancel()
	<-done
}

// TestStreamSendsSystemData verifies that at least one MsgSystem message is
// received during a short run (a few ticks).
func TestStreamSendsSystemData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	dec, err := stream.NewDecoder(server)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	msgs := make(chan *stream.Message, 128)
	go readMessages(dec, msgs)

	// Use a short interval so the test completes quickly.
	interval := 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- StreamTelemetry(ctx, client, interval)
	}()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				t.Fatal("message channel closed before receiving MsgSystem")
			}
			if msg.Type == stream.MsgSystem {
				cancel()
				<-done
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for MsgSystem message")
		}
	}
}

// TestStreamStopsOnCancel verifies that cancelling the context causes
// StreamTelemetry to return nil (clean shutdown, not an error).
func TestStreamStopsOnCancel(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Drain the server side so writes don't block.
	dec, err := stream.NewDecoder(server)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	msgs := make(chan *stream.Message, 128)
	go readMessages(dec, msgs)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- StreamTelemetry(ctx, client, 100*time.Millisecond)
	}()

	// Let it run for a moment then cancel.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StreamTelemetry returned non-nil error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StreamTelemetry did not return after context cancellation")
	}
}
