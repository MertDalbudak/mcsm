// Command probe-ws connects to a mcsm WebSocket endpoint, prints
// frames it receives, and exits after a configurable count or timeout.
// Used by the smoke test scripts; not shipped.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
)

func main() {
	url := flag.String("url", "", "ws:// URL")
	token := flag.String("token", "", "bearer token (optional)")
	frames := flag.Int("frames", 5, "stop after N frames")
	timeout := flag.Duration("timeout", 10*time.Second, "overall timeout")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "usage: probe-ws --url ws://... [--token X] [--frames N]")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	hdr := http.Header{}
	if *token != "" {
		hdr.Set("Authorization", "Bearer "+*token)
	}
	c, _, err := websocket.Dial(ctx, *url, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer c.CloseNow()

	for i := 0; i < *frames; i++ {
		_, body, err := c.Read(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			break
		}
		fmt.Printf("frame: %s\n", string(body))
	}
}
