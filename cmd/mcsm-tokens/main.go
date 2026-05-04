// Command mcsm-tokens generates argon2id PHC strings suitable for the
// api.tokens[].hash field in config.yaml. Reads the plaintext token
// from stdin (so it never appears in shell history or argv).
//
// Usage:
//
//	echo -n 'my-secret' | mcsm-tokens
//	mcsm-tokens --random            # generates 32 hex chars and prints both
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/argon2"
)

func main() {
	random := flag.Bool("random", false, "generate a random 32-hex-char token, print both plaintext and hash")
	m := flag.Uint("m", 65536, "argon2id memory cost (KiB)")
	t := flag.Uint("t", 3, "argon2id time cost")
	p := flag.Uint("p", 2, "argon2id parallelism")
	flag.Parse()

	var plaintext string
	if *random {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			fail("rand: %v", err)
		}
		plaintext = hex.EncodeToString(raw)
		fmt.Fprintf(os.Stderr, "plaintext token (give to your client; not stored):\n  %s\n\n", plaintext)
	} else {
		// Read from stdin, strip trailing newline only (don't trim
		// other whitespace — operators may have intentional spaces).
		body, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fail("read stdin: %v", err)
		}
		plaintext = strings.TrimRight(string(body), "\n")
		if plaintext == "" {
			fail("empty token (pipe one in: `echo -n my-token | mcsm-tokens`)")
		}
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		fail("salt: %v", err)
	}
	hash := argon2.IDKey([]byte(plaintext), salt, uint32(*t), uint32(*m), uint8(*p), 32)
	fmt.Printf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s\n",
		*m, *t, *p,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mcsm-tokens: "+format+"\n", args...)
	os.Exit(1)
}
