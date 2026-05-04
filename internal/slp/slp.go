// Package slp implements a minimal Server List Ping client for the modern
// Java Edition protocol. It performs the handshake → status → ping
// exchange and returns the parsed status JSON plus measured latency.
//
// Reference: https://wiki.vg/Server_List_Ping (Modern protocol; works for
// every Java release ≥ 1.7).
//
// We deliberately do not use a Minecraft library — this is ~150 lines of
// length-prefixed bytes and one VarInt parser, and avoiding a dep keeps
// the binary small.
package slp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Status is the parsed response from a status query. The full server JSON
// is preserved in Raw for callers that want fields we don't surface.
type Status struct {
	Version     Version         `json:"version"`
	Players     Players         `json:"players"`
	Description any             `json:"description"` // can be a chat component object or string
	Favicon     string          `json:"favicon,omitempty"`
	Raw         json.RawMessage `json:"-"`
	LatencyMS   int64           `json:"-"`
}

type Version struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type Players struct {
	Max    int            `json:"max"`
	Online int            `json:"online"`
	Sample []PlayerSample `json:"sample,omitempty"`
}

type PlayerSample struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// Probe performs the SLP exchange against host:port within ctx. The
// timeout for the whole operation should come from the context (use
// context.WithTimeout in callers).
func Probe(ctx context.Context, host string, port int) (*Status, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("slp dial %s: %w", addr, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	// --- Handshake (next state = 1, status) ---
	hs := buildPacket(0x00,
		writeVarInt(-1),     // protocol version (-1 = "unknown / status query")
		writeString(host),
		writeUint16(uint16(port)),
		writeVarInt(1),      // next state: status
	)
	if _, err := conn.Write(hs); err != nil {
		return nil, fmt.Errorf("slp handshake write: %w", err)
	}

	// --- Status request ---
	if _, err := conn.Write(buildPacket(0x00)); err != nil {
		return nil, fmt.Errorf("slp status write: %w", err)
	}

	// --- Read status response ---
	pktLen, err := readVarInt(conn)
	if err != nil {
		return nil, fmt.Errorf("slp read length: %w", err)
	}
	body := make([]byte, pktLen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("slp read body: %w", err)
	}

	r := &byteReader{buf: body}
	pid, err := r.varInt()
	if err != nil {
		return nil, fmt.Errorf("slp parse packet id: %w", err)
	}
	if pid != 0x00 {
		return nil, fmt.Errorf("slp: unexpected packet id 0x%x", pid)
	}
	jsonLen, err := r.varInt()
	if err != nil {
		return nil, fmt.Errorf("slp parse json length: %w", err)
	}
	if int(jsonLen) > len(r.buf)-r.pos {
		return nil, errors.New("slp: status json length exceeds packet body")
	}
	jsonBytes := r.buf[r.pos : r.pos+int(jsonLen)]

	var st Status
	if err := json.Unmarshal(jsonBytes, &st); err != nil {
		return nil, fmt.Errorf("slp parse status json: %w", err)
	}
	st.Raw = append(json.RawMessage(nil), jsonBytes...)

	// --- Optional ping for latency ---
	pingPayload := uint64(time.Now().UnixNano())
	pb := make([]byte, 8)
	binary.BigEndian.PutUint64(pb, pingPayload)
	pingStart := time.Now()
	if _, err := conn.Write(buildPacket(0x01, pb)); err == nil {
		_, _ = readVarInt(conn) // length
		respID, err := readVarInt(conn)
		if err == nil && respID == 0x01 {
			// drain 8 bytes
			pong := make([]byte, 8)
			if _, err := io.ReadFull(conn, pong); err == nil {
				st.LatencyMS = time.Since(pingStart).Milliseconds()
			}
		}
	}
	return &st, nil
}

// --- Wire helpers ---

func buildPacket(id byte, parts ...[]byte) []byte {
	body := []byte{}
	body = append(body, writeVarInt(int32(id))...)
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

func writeUint16(v uint16) []byte {
	return []byte{byte(v >> 8), byte(v)}
}

// readVarInt reads from an io.Reader (used for the outer packet length).
func readVarInt(r io.Reader) (int32, error) {
	var (
		num    uint32
		buf    [1]byte
		shift  uint
	)
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
	return 0, errors.New("varint too long")
}

type byteReader struct {
	buf []byte
	pos int
}

func (b *byteReader) varInt() (int32, error) {
	var (
		num   uint32
		shift uint
	)
	for i := 0; i < 5; i++ {
		if b.pos >= len(b.buf) {
			return 0, io.ErrUnexpectedEOF
		}
		c := b.buf[b.pos]
		b.pos++
		num |= uint32(c&0x7F) << shift
		if c&0x80 == 0 {
			return int32(num), nil
		}
		shift += 7
	}
	return 0, errors.New("varint too long")
}
