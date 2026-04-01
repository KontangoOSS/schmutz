package clienthello

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Info holds metadata extracted from a TLS ClientHello message.
type Info struct {
	SNI          string
	TLSVersion   uint16
	CipherSuites []uint16
	Extensions   []uint16
	ALPNProtos   []string
	Raw          []byte // Full ClientHello bytes for JA4 computation
}

// Peek reads the TLS ClientHello from conn without consuming it.
// Returns the parsed info and a net.Conn that replays the peeked bytes
// followed by the rest of the connection.
func Peek(conn net.Conn, timeout time.Duration) (*Info, net.Conn, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, conn, fmt.Errorf("set deadline: %w", err)
	}

	// TLS record header is 5 bytes: type(1) + version(2) + length(2)
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, conn, fmt.Errorf("read record header: %w", err)
	}

	// Verify this is a TLS Handshake record (type 0x16)
	if header[0] != 0x16 {
		return nil, conn, errors.New("not a TLS handshake")
	}

	// Record payload length
	length := int(header[3])<<8 | int(header[4])
	if length > 16384 {
		return nil, conn, errors.New("record too large")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, conn, fmt.Errorf("read record payload: %w", err)
	}

	// Clear the deadline for the relay phase
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, conn, fmt.Errorf("clear deadline: %w", err)
	}

	// Combine header + payload for replay
	raw := append(header, payload...)

	info, err := parse(payload)
	if err != nil {
		return nil, newReplayConn(conn, raw), fmt.Errorf("parse clienthello: %w", err)
	}
	info.Raw = raw

	return info, newReplayConn(conn, raw), nil
}

// parse extracts fields from the ClientHello handshake message body.
func parse(payload []byte) (*Info, error) {
	if len(payload) < 6 {
		return nil, errors.New("payload too short")
	}

	// Handshake type: ClientHello = 0x01
	if payload[0] != 0x01 {
		return nil, fmt.Errorf("not a ClientHello (type=%d)", payload[0])
	}

	// Skip handshake header: type(1) + length(3)
	msg := payload[4:]
	info := &Info{}

	if len(msg) < 2 {
		return nil, errors.New("missing client version")
	}
	info.TLSVersion = uint16(msg[0])<<8 | uint16(msg[1])
	msg = msg[2:]

	// Skip random (32 bytes)
	if len(msg) < 32 {
		return nil, errors.New("missing random")
	}
	msg = msg[32:]

	// Skip session ID
	if len(msg) < 1 {
		return nil, errors.New("missing session id length")
	}
	sessLen := int(msg[0])
	msg = msg[1:]
	if len(msg) < sessLen {
		return nil, errors.New("session id truncated")
	}
	msg = msg[sessLen:]

	// Cipher suites
	if len(msg) < 2 {
		return nil, errors.New("missing cipher suites length")
	}
	csLen := int(msg[0])<<8 | int(msg[1])
	msg = msg[2:]
	if len(msg) < csLen || csLen%2 != 0 {
		return nil, errors.New("cipher suites truncated")
	}
	for i := 0; i < csLen; i += 2 {
		cs := uint16(msg[i])<<8 | uint16(msg[i+1])
		// Skip GREASE values
		if cs&0x0f0f == 0x0a0a {
			continue
		}
		info.CipherSuites = append(info.CipherSuites, cs)
	}
	msg = msg[csLen:]

	// Skip compression methods
	if len(msg) < 1 {
		return nil, errors.New("missing compression methods")
	}
	compLen := int(msg[0])
	msg = msg[1:]
	if len(msg) < compLen {
		return nil, errors.New("compression methods truncated")
	}
	msg = msg[compLen:]

	// Extensions
	if len(msg) < 2 {
		return info, nil // No extensions
	}
	extLen := int(msg[0])<<8 | int(msg[1])
	msg = msg[2:]
	if len(msg) < extLen {
		return nil, errors.New("extensions truncated")
	}

	extData := msg[:extLen]
	for len(extData) >= 4 {
		extType := uint16(extData[0])<<8 | uint16(extData[1])
		extBodyLen := int(extData[2])<<8 | int(extData[3])
		extData = extData[4:]
		if len(extData) < extBodyLen {
			break
		}
		body := extData[:extBodyLen]
		extData = extData[extBodyLen:]

		// Skip GREASE extension types
		if extType&0x0f0f == 0x0a0a {
			continue
		}
		info.Extensions = append(info.Extensions, extType)

		switch extType {
		case 0x0000: // SNI
			info.SNI = parseSNI(body)
		case 0x0010: // ALPN
			info.ALPNProtos = parseALPN(body)
		}
	}

	return info, nil
}

func parseSNI(data []byte) string {
	if len(data) < 5 {
		return ""
	}
	// Skip SNI list length (2 bytes)
	data = data[2:]
	// Type must be 0x00 (hostname)
	if data[0] != 0x00 {
		return ""
	}
	nameLen := int(data[1])<<8 | int(data[2])
	data = data[3:]
	if len(data) < nameLen {
		return ""
	}
	return string(data[:nameLen])
}

func parseALPN(data []byte) []string {
	if len(data) < 2 {
		return nil
	}
	// Skip ALPN list length (2 bytes)
	data = data[2:]
	var protos []string
	for len(data) > 0 {
		pLen := int(data[0])
		data = data[1:]
		if len(data) < pLen {
			break
		}
		protos = append(protos, string(data[:pLen]))
		data = data[pLen:]
	}
	return protos
}

// replayConn wraps a net.Conn and prepends buffered bytes before the live stream.
type replayConn struct {
	net.Conn
	buf []byte
	pos int
}

func newReplayConn(conn net.Conn, buf []byte) net.Conn {
	return &replayConn{Conn: conn, buf: buf}
}

func (c *replayConn) Read(p []byte) (int, error) {
	if c.pos < len(c.buf) {
		n := copy(p, c.buf[c.pos:])
		c.pos += n
		return n, nil
	}
	return c.Conn.Read(p)
}
