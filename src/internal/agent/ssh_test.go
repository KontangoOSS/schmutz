package agent

import (
	"io"
	"net"
	"testing"
	"time"
)

// TestSSHProxyConnects starts a mock TCP server (echo), starts the proxy
// pointed at it, dials the proxy, sends data, and verifies it echoes back.
func TestSSHProxyConnects(t *testing.T) {
	// Start mock "SSH" server that echoes whatever it receives.
	mockLn, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer mockLn.Close()
	mockPort := mockLn.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := mockLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) //nolint:errcheck — echo
			}(conn)
		}
	}()

	// Start proxy listener.
	proxyLn, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}
	defer proxyLn.Close()

	go ServeSSHProxy(proxyLn, mockPort) //nolint:errcheck

	// Connect to proxy.
	conn, err := net.DialTimeout("tcp", proxyLn.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatalf("failed to dial proxy: %v", err)
	}
	defer conn.Close()

	// Send some data and expect it echoed back.
	want := []byte("hello schmutz ssh proxy")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestSSHProxyHandlesDisconnect verifies that a client that connects and
// immediately closes does not cause a panic or hang.
func TestSSHProxyHandlesDisconnect(t *testing.T) {
	// Start mock server.
	mockLn, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer mockLn.Close()
	mockPort := mockLn.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := mockLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) //nolint:errcheck
			}(conn)
		}
	}()

	// Start proxy.
	proxyLn, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}
	defer proxyLn.Close()

	go ServeSSHProxy(proxyLn, mockPort) //nolint:errcheck

	// Connect and immediately close — should not panic.
	conn, err := net.DialTimeout("tcp", proxyLn.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatalf("failed to dial proxy: %v", err)
	}
	conn.Close()

	// Give the proxy goroutine a moment to handle the disconnect.
	time.Sleep(100 * time.Millisecond)
	// If we reach here without a panic, the test passes.
}
