package beacon

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/google/uuid"
)

// crockfordAlphabet excludes 0, O, 1, I, L for human readability.
const crockfordAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

// ShortCode derives a stable, human-readable 9-char (XXXX-XXXX) code from a UUID.
// Same UUID always produces the same code. Different UUIDs almost always produce
// different codes (≈30^8 ≈ 6.5e11 combinations).
func ShortCode(u uuid.UUID) string {
	h := sha256.Sum256(u[:])
	// Take first 5 bytes (40 bits) → 8 base-30 chars.
	v := binary.BigEndian.Uint64(h[:8]) >> 24
	out := make([]byte, 9)
	for i := 7; i >= 0; i-- {
		idx := i
		if i >= 4 {
			idx = i + 1 // skip dash position
		}
		out[idx] = crockfordAlphabet[v%30]
		v /= 30
	}
	out[4] = '-'
	return string(out)
}
