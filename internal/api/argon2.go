package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// argonHash holds the parsed components of a PHC argon2id string:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt-b64>$<hash-b64>
type argonHash struct {
	m, t   uint32
	p      uint8
	salt   []byte
	hash   []byte
	keyLen uint32
}

// parseArgon2id parses a PHC-encoded argon2id hash. We accept v=19 only.
func parseArgon2id(s string) (*argonHash, error) {
	parts := strings.Split(s, "$")
	// Leading "$" produces an empty first element.
	// Expected layout: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 {
		return nil, fmt.Errorf("argon2id: expected 6 fields, got %d", len(parts))
	}
	if parts[1] != "argon2id" {
		return nil, fmt.Errorf("argon2id: algorithm must be argon2id, got %q", parts[1])
	}
	if parts[2] != "v=19" {
		return nil, fmt.Errorf("argon2id: only version 19 supported, got %q", parts[2])
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return nil, fmt.Errorf("argon2id: parse params %q: %w", parts[3], err)
	}
	if m == 0 || t == 0 || p == 0 {
		return nil, errors.New("argon2id: m, t, p must be > 0")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, fmt.Errorf("argon2id: decode salt: %w", err)
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, fmt.Errorf("argon2id: decode hash: %w", err)
	}
	return &argonHash{
		m: m, t: t, p: p,
		salt: salt, hash: hash, keyLen: uint32(len(hash)),
	}, nil
}
