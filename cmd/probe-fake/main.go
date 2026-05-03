// Command probe-fake exercises mcsm's SLP and RCON clients against a
// running fake-mc-server. Used during development; not shipped.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MertDalbudak/mcsm/internal/rcon"
	"github.com/MertDalbudak/mcsm/internal/slp"
)

func main() {
	port := 25599
	rconPort := 35599
	pass := "testpass"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := slp.Probe(ctx, "127.0.0.1", port)
	if err != nil {
		fmt.Println("SLP error:", err)
	} else {
		fmt.Printf("SLP ok: motd=%v players=%d/%d latency=%dms\n",
			st.Description, st.Players.Online, st.Players.Max, st.LatencyMS)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	c, err := rcon.Dial(ctx2, "127.0.0.1", rconPort, pass)
	if err != nil {
		fmt.Println("RCON dial error:", err)
		return
	}
	defer c.Close()
	resp, err := c.Cmd(context.Background(), "list")
	if err != nil {
		fmt.Println("RCON cmd error:", err)
	} else {
		fmt.Printf("RCON list: %q\n", resp)
	}
	resp, err = c.Cmd(context.Background(), "say hello")
	if err != nil {
		fmt.Println("RCON say error:", err)
	} else {
		fmt.Printf("RCON say: %q\n", resp)
	}
}
