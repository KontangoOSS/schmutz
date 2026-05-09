package clienthello

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// JA4 computes the JA4 fingerprint from a parsed ClientHello.
//
// JA4 format: {proto}{version}{sni}{ciphers_count}{ext_count}_{cipher_hash}_{ext_hash}
//
// This is a simplified implementation following the JA4 specification.
// See: https://github.com/FoxIO-LLC/ja4
func JA4(info *Info) string {
	if info == nil {
		return ""
	}

	// Protocol: t = TCP (we only handle TCP)
	proto := "t"

	// TLS version: map to 2-char code
	version := tlsVersionCode(info.TLSVersion, info.Extensions)

	// SNI indicator: d = domain present, i = IP or absent
	sniChar := "i"
	if info.SNI != "" {
		sniChar = "d"
	}

	// Cipher suite count (2 digits, capped at 99)
	csCount := len(info.CipherSuites)
	if csCount > 99 {
		csCount = 99
	}

	// Extension count (2 digits, capped at 99)
	extCount := len(info.Extensions)
	if extCount > 99 {
		extCount = 99
	}

	// ALPN first value
	alpn := "00"
	if len(info.ALPNProtos) > 0 {
		p := info.ALPNProtos[0]
		if len(p) >= 2 {
			alpn = p[:2]
		}
	}

	// Section A: protocol + version + SNI + cipher count + extension count + ALPN
	sectionA := fmt.Sprintf("%s%s%s%02d%02d%s", proto, version, sniChar, csCount, extCount, alpn)

	// Section B: sorted cipher suites, SHA256 truncated to 12 hex chars
	sectionB := hashSorted(info.CipherSuites)

	// Section C: sorted extensions (excluding SNI 0x0000 and ALPN 0x0010), SHA256 truncated
	filteredExts := filterExtensions(info.Extensions)
	sectionC := hashSorted(filteredExts)

	return fmt.Sprintf("%s_%s_%s", sectionA, sectionB, sectionC)
}

func tlsVersionCode(clientVersion uint16, extensions []uint16) string {
	// Check supported_versions extension for actual version
	// For now, use the ClientHello version field
	switch clientVersion {
	case 0x0304:
		return "13" // TLS 1.3
	case 0x0303:
		return "12" // TLS 1.2
	case 0x0302:
		return "11" // TLS 1.1
	case 0x0301:
		return "10" // TLS 1.0
	default:
		return "00"
	}
}

func filterExtensions(exts []uint16) []uint16 {
	var filtered []uint16
	for _, e := range exts {
		// Exclude SNI (0x0000) and ALPN (0x0010) from the hash
		if e == 0x0000 || e == 0x0010 {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

func hashSorted(values []uint16) string {
	if len(values) == 0 {
		return "000000000000"
	}

	sorted := make([]uint16, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var parts []string
	for _, v := range sorted {
		parts = append(parts, fmt.Sprintf("%04x", v))
	}

	h := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return fmt.Sprintf("%x", h[:6])
}
