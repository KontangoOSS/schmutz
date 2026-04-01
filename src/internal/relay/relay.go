package relay

import (
	"io"
	"net"
	"sync"
)

// Bidirectional copies bytes between two connections until one side closes or errors.
// Returns total bytes transferred in each direction.
func Bidirectional(client, backend net.Conn) (bytesIn, bytesOut int64) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Client → Backend (request direction, "bytes in" to the backend)
	go func() {
		defer wg.Done()
		bytesIn, _ = io.Copy(backend, client)
		// Signal backend that client is done writing
		if tc, ok := backend.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
	}()

	// Backend → Client (response direction, "bytes out" from the backend)
	go func() {
		defer wg.Done()
		bytesOut, _ = io.Copy(client, backend)
		// Signal client that backend is done writing
		if tc, ok := client.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
	return bytesIn, bytesOut
}
