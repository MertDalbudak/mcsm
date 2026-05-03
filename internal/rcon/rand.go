package rcon

import "crypto/rand"

// readRand is split out so tests can stub it. Production reads from
// crypto/rand directly.
var readRand = func(b []byte) (int, error) { return rand.Read(b) }
