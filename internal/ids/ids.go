// Package ids generates UUIDv7 identifiers and request trace ids.
package ids

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/google/uuid"
)

// NewServerID returns a UUIDv7 string used as a server's stable identity.
// Falls back to UUIDv4 if the v7 generator fails (it shouldn't).
func NewServerID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

// NewTraceID returns a 16-hex-char (8-byte) identifier suitable for
// request correlation. Short enough to scan, large enough to not collide
// in any reasonable request volume.
func NewTraceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
