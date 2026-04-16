package agent

import (
	"fmt"
	"io"
	"log/slog"
	"net"
)

// ServeSSHProxy accepts connections from the listener and proxies each to
// localhost:<sshPort>. Blocks until the listener is closed.
func ServeSSHProxy(ln net.Listener, sshPort int) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed — clean exit.
			return err
		}
		go handleSSHConn(conn, sshPort)
	}
}

// handleSSHConn dials localhost:<sshPort> and pipes bidirectionally.
func handleSSHConn(client net.Conn, sshPort int) {
	defer client.Close()

	target, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", sshPort))
	if err != nil {
		slog.Warn("ssh proxy: dial failed", "port", sshPort, "error", err)
		return
	}
	defer target.Close()

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(target, client) //nolint:errcheck
		done <- struct{}{}
	}()
	go func() {
		io.Copy(client, target) //nolint:errcheck
		done <- struct{}{}
	}()

	// Wait for either direction to finish, then close both connections so the
	// other goroutine unblocks.
	<-done
}
