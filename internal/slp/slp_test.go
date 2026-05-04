package slp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// stubSLPServer answers a single SLP exchange and exits. Used to verify
// the client's wire format and JSON parsing.
func stubSLPServer(t *testing.T) (host string, port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop = func() { ln.Close() }
	a := ln.Addr().(*net.TCPAddr)
	host = "127.0.0.1"
	port = a.Port
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		// Drain handshake + status request.
		for i := 0; i < 2; i++ {
			length, err := readVarIntBlocking(c)
			if err != nil {
				return
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(c, body); err != nil {
				return
			}
		}
		// Send status response.
		body, _ := json.Marshal(map[string]any{
			"version":     map[string]any{"name": "test 1.0", "protocol": 100},
			"players":     map[string]any{"max": 20, "online": 3, "sample": []any{}},
			"description": "stub motd",
		})
		pkt := buildPacketTest(0x00, writeStringTest(string(body)))
		c.Write(pkt)
		// Optional ping/pong: read 8-byte payload after VarInt.
		length, _ := readVarIntBlocking(c)
		body2 := make([]byte, length)
		io.ReadFull(c, body2)
		if len(body2) >= 9 {
			pong := buildPacketTest(0x01, body2[1:9])
			c.Write(pong)
		}
	}()
	return
}

func buildPacketTest(id byte, parts ...[]byte) []byte {
	body := []byte{id}
	for _, p := range parts {
		body = append(body, p...)
	}
	return append(writeVarIntTest(int32(len(body))), body...)
}

func writeVarIntTest(v int32) []byte {
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

func writeStringTest(s string) []byte {
	b := []byte(s)
	return append(writeVarIntTest(int32(len(b))), b...)
}

func readVarIntBlocking(r io.Reader) (uint32, error) {
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
			return num, nil
		}
		shift += 7
	}
	return 0, errors.New("varint too long")
}

func TestProbe_ParsesResponse(t *testing.T) {
	host, port, stop := stubSLPServer(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, err := Probe(ctx, host, port)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st.Players.Online != 3 || st.Players.Max != 20 {
		t.Errorf("players: %+v", st.Players)
	}
	if st.Version.Name != "test 1.0" || st.Version.Protocol != 100 {
		t.Errorf("version: %+v", st.Version)
	}
	if d, _ := st.Description.(string); d != "stub motd" {
		t.Errorf("motd: %v", st.Description)
	}
}

func TestProbe_ConnectionRefused(t *testing.T) {
	// Pick an unused high port.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Probe(ctx, "127.0.0.1", 1)
	if err == nil {
		t.Error("expected dial error")
	}
}

// _ silences unused; binary import is needed by stub closure.
var _ = binary.LittleEndian
