package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// makeHash produces an argon2id PHC string for the given password using
// fixed parameters. Used by multiple test files in this package.
func makeHash(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	const m, ti, p uint32 = 65536, 3, 2
	const keyLen = uint32(32)
	hash := argon2.IDKey([]byte(password), salt, ti, m, uint8(p), keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		m, ti, p,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
}

func TestParseArgon2id_Roundtrip(t *testing.T) {
	h := makeHash(t, "hunter2")
	parsed, err := parseArgon2id(h)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.t == 0 || parsed.m == 0 || parsed.p == 0 {
		t.Errorf("zero params: %+v", parsed)
	}
	if len(parsed.salt) != 16 {
		t.Errorf("salt length: %d", len(parsed.salt))
	}
	if parsed.keyLen != uint32(len(parsed.hash)) {
		t.Errorf("keyLen mismatch: %d vs %d", parsed.keyLen, len(parsed.hash))
	}
}

func TestParseArgon2id_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "expected 6 fields"},
		{"wrong_alg", "$argon2i$v=19$m=65536,t=3,p=2$AAAA$BBBB", "must be argon2id"},
		{"wrong_version", "$argon2id$v=18$m=65536,t=3,p=2$AAAA$BBBB", "version 19"},
		{"bad_params", "$argon2id$v=19$xxx$AAAA$BBBB", "parse params"},
		{"zero_params", "$argon2id$v=19$m=0,t=3,p=2$AAAA$BBBB", "must be > 0"},
		{"bad_salt_b64", "$argon2id$v=19$m=65536,t=3,p=2$!!!$BBBB", "decode salt"},
		{"bad_hash_b64", "$argon2id$v=19$m=65536,t=3,p=2$AAAA$!!!", "decode hash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseArgon2id(tc.in)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want substring %q", err, tc.want)
			}
		})
	}
}
