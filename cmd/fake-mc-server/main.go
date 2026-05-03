// Command fake-mc-server is a tiny stand-in for a real Minecraft Java
// Edition server, used by integration smoke tests for mcsm. It speaks
// just enough of the Server List Ping and Source RCON protocols to let
// the slot state machine drive a full mount → start → stop → idle cycle
// without bringing up a real JVM.
//
// The slot manager invokes "java <args> -jar <jar> nogui" from a server
// directory. To masquerade as java for tests, point PATH at a directory
// containing a `java` symlink to this binary (or a shell shim that
// execs it). The binary ignores its CLI args and just reads
// server.properties from cwd.
//
// Behavior:
//
//   - Reads server.properties for server-port, rcon.port, rcon.password.
//   - Listens on server-port for SLP (handshake → status → optional ping).
//   - Listens on rcon.port for RCON commands.
//   - Recognized commands: list, say <msg>, stop. Anything else echoes back.
//   - Exits cleanly on "stop", on SIGTERM, or on SIGINT.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	props, err := readProps("server.properties")
	if err != nil {
		log.Fatalf("fake-mc-server: read server.properties: %v", err)
	}
	port, _ := strconv.Atoi(props["server-port"])
	rconPort, _ := strconv.Atoi(props["rcon.port"])
	rconPass := props["rcon.password"]
	if port == 0 || rconPort == 0 || rconPass == "" {
		log.Fatalf("fake-mc-server: missing server-port/rcon.port/rcon.password")
	}

	stopCh := make(chan struct{})
	var stopped atomic.Bool

	// SLP listener
	slpLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Fatalf("fake-mc-server: bind SLP %d: %v", port, err)
	}
	defer slpLn.Close()
	go acceptSLP(slpLn, port, stopCh)

	// RCON listener
	rconLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", rconPort))
	if err != nil {
		log.Fatalf("fake-mc-server: bind RCON %d: %v", rconPort, err)
	}
	defer rconLn.Close()
	go acceptRCON(rconLn, rconPass, stopCh, &stopped)

	log.Printf("fake-mc-server: ready on slp=%d rcon=%d", port, rconPort)

	// SIGTERM/SIGINT also count as graceful exit.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sig:
		log.Print("fake-mc-server: signal received, exiting")
	case <-stopCh:
		log.Print("fake-mc-server: stop command received, exiting")
	}
	stopped.Store(true)
	// Brief drain.
	time.Sleep(100 * time.Millisecond)
}

func readProps(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			continue
		}
		out[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
	}
	return out, sc.Err()
}

// --- SLP ---

func acceptSLP(ln net.Listener, port int, stopCh <-chan struct{}) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				log.Printf("fake-mc-server: SLP accept: %v", err)
				return
			}
		}
		go serveSLP(conn, port)
	}
}

func serveSLP(conn net.Conn, port int) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Each MC packet on the wire is: VarInt length, then `length` bytes of body.
	// The first byte of body is the packet id (also a VarInt, but always
	// single-byte for these). For SLP we get:
	//   1) Handshake (id 0x00)
	//   2) Status request (id 0x00, empty payload)
	//   3) [we send Status response (id 0x00, JSON payload)]
	//   4) Optional ping (id 0x01, 8-byte payload)
	//   5) [we send Pong (id 0x01, echoed 8 bytes)]
	if _, err := readPacket(conn); err != nil {
		return
	}
	if _, err := readPacket(conn); err != nil {
		return
	}

	status := map[string]any{
		"version":     map[string]any{"name": "fake 1.21.4", "protocol": 768},
		"players":     map[string]any{"max": 20, "online": 0, "sample": []any{}},
		"description": "fake mcsm test server",
	}
	body, _ := json.Marshal(status)
	pkt := buildPacket(0x00, writeString(string(body)))
	if _, err := conn.Write(pkt); err != nil {
		return
	}

	conn.SetDeadline(time.Now().Add(2 * time.Second))
	pingBody, err := readPacket(conn)
	if err == nil && len(pingBody) >= 9 {
		// pingBody[0] is packet id (0x01); echo the next 8 bytes.
		pong := buildPacket(0x01, pingBody[1:9])
		_, _ = conn.Write(pong)
	}
}

// readPacket reads one length-prefixed Minecraft packet body and
// returns it (with the leading packet-id byte still present at body[0]).
func readPacket(r io.Reader) ([]byte, error) {
	length, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if length < 0 || length > 1<<20 {
		return nil, fmt.Errorf("implausible packet length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func buildPacket(id byte, parts ...[]byte) []byte {
	body := []byte{id}
	for _, p := range parts {
		body = append(body, p...)
	}
	return append(writeVarInt(int32(len(body))), body...)
}

func writeVarInt(v int32) []byte {
	var out []byte
	uv := uint32(v)
	for {
		if uv&^0x7F == 0 {
			return append(out, byte(uv))
		}
		out = append(out, byte(uv&0x7F|0x80))
		uv >>= 7
	}
}

func writeString(s string) []byte {
	b := []byte(s)
	return append(writeVarInt(int32(len(b))), b...)
}

func readVarInt(r io.Reader) (int32, error) {
	var num uint32
	var buf [1]byte
	var shift uint
	for i := 0; i < 5; i++ {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		b := buf[0]
		num |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return int32(num), nil
		}
		shift += 7
	}
	return 0, fmt.Errorf("varint too long")
}

// --- RCON ---

const (
	rconLogin    = 3
	rconExec     = 2
	rconResponse = 0
)

func acceptRCON(ln net.Listener, password string, stopCh chan struct{}, stopped *atomic.Bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if stopped.Load() {
				return
			}
			log.Printf("fake-mc-server: RCON accept: %v", err)
			return
		}
		go serveRCON(conn, password, stopCh)
	}
}

func serveRCON(conn net.Conn, password string, stopCh chan struct{}) {
	defer conn.Close()
	authenticated := false
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		id, ptype, payload, err := readRconPkt(conn)
		if err != nil {
			return
		}
		switch ptype {
		case rconLogin:
			if payload == password {
				authenticated = true
				_ = writeRconPkt(conn, id, rconExec, "")
			} else {
				_ = writeRconPkt(conn, -1, rconExec, "")
				return
			}
		case rconExec:
			if !authenticated {
				return
			}
			resp := handleCmd(payload, stopCh)
			_ = writeRconPkt(conn, id, rconResponse, resp)
		}
	}
}

func handleCmd(cmd string, stopCh chan struct{}) string {
	switch {
	case cmd == "stop":
		// Trigger graceful shutdown.
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		return "Stopping the server"
	case cmd == "list":
		return "There are 0 of a max of 20 players online: "
	case strings.HasPrefix(cmd, "say "):
		return ""
	case strings.HasPrefix(cmd, "kick "):
		return "No player was found"
	case cmd == "whitelist reload":
		return "Reloaded the whitelist"
	default:
		return "Echoed: " + cmd
	}
}

func readRconPkt(r io.Reader) (id, ptype int32, payload string, err error) {
	var lenBuf [4]byte
	if _, err = io.ReadFull(r, lenBuf[:]); err != nil {
		return
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	body := make([]byte, length)
	if _, err = io.ReadFull(r, body); err != nil {
		return
	}
	id = int32(binary.LittleEndian.Uint32(body[0:4]))
	ptype = int32(binary.LittleEndian.Uint32(body[4:8]))
	rest := body[8:]
	for i, b := range rest {
		if b == 0 {
			payload = string(rest[:i])
			return
		}
	}
	payload = string(rest)
	return
}

func writeRconPkt(w io.Writer, id, ptype int32, payload string) error {
	body := []byte(payload)
	length := int32(4 + 4 + len(body) + 2)
	buf := make([]byte, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(id))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(ptype))
	copy(buf[12:], body)
	_, err := w.Write(buf)
	return err
}
