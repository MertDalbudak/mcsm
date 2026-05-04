package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// renderHash inlines the same logic as main() so we can verify the
// emitted PHC string round-trips correctly.
func renderHash(plaintext string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plaintext), salt, 3, 65536, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		65536, 3, 2,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func TestRoundtrip(t *testing.T) {
	pw := "smoke-token-please-change"
	got, err := renderHash(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "$argon2id$v=19$") {
		t.Errorf("prefix: %s", got)
	}
	parts := strings.Split(got, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 fields, got %d in %q", len(parts), got)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("salt b64: %v", err)
	}
	stored, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Fatalf("hash b64: %v", err)
	}
	derived := argon2.IDKey([]byte(pw), salt, 3, 65536, 2, 32)
	if !bytes.Equal(derived, stored) {
		t.Errorf("re-derived hash doesn't match stored")
	}
}
