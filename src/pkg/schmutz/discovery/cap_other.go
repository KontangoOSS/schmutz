//go:build !linux

package discovery

// capPresent is a no-op on non-Linux platforms. macOS and Windows don't expose
// capabilities the way Linux does; the agent reports "no" for capability bits
// and falls back to UID-only privilege detection.
func capPresent(_ string) bool { return false }
