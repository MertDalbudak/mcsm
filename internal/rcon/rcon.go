// Package rcon implements the Source RCON protocol used by Minecraft
// Java Edition (Vanilla, Paper, Spigot, Forge, Fabric).
//
// Protocol reference: https://wiki.vg/RCON
//
//   Packet wire format (little-endian):
//     int32 length    // remaining bytes in this packet
//     int32 id        // request id (echoed in response)
//     int32 type      // 3 = login, 2 = exec command, 0 = response
//     bytes payload   // null-terminated string
//     byte  pad       // null byte
//
// We keep one TCP connection per server, gated by a mutex so handlers
// don't multiplex commands on the same socket.
package rcon

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	typeLogin    = 3
	typeExecCmd  = 2
	typeResponse = 0
	typeAuthResp = 2 // the server uses type 2 for the auth response

	maxPayload = 1413 // server-side limit; see wiki.vg
)

// ErrAuthFailed is returned when the password is wrong.
var ErrAuthFailed = errors.New("rcon: authentication failed")

// ErrClosed is returned for operations on a closed client.
var ErrClosed = errors.New("rcon: client is closed")

// Client is a single connection to a server's RCON listener.
//
// Construct with Dial. Methods are goroutine-safe; commands are
// serialized so RCON sees one in flight at a time.
type Client struct {
	conn net.Conn
	mu   sync.Mutex
	id   atomic.Int32
	addr string

	closed atomic.Bool
}

// Dial opens a TCP connection to host:port and authenticates with the
// given password. ctx bounds the entire dial+auth sequence.
func Dial(ctx context.Context, host string, port int, password string) (*Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rcon dial %s: %w", addr, err)
	}
	c := &Client{conn: conn, addr: addr}
	c.id.Store(0)
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if err := c.authenticate(password); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return c, nil
}

// Cmd executes a Minecraft console command and returns the response
// (which may be empty for commands like "save-all"). Subject to the
// 1413-byte payload limit; longer commands are rejected.
//
// Pass a context with a deadline to bound how long this can block; the
// underlying socket will be deadline-set for the duration.
func (c *Client) Cmd(ctx context.Context, command string) (string, error) {
	if c.closed.Load() {
		return "", ErrClosed
	}
	if len(command) > maxPayload {
		return "", fmt.Errorf("rcon: command exceeds %d bytes", maxPayload)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(dl)
		defer c.conn.SetDeadline(time.Time{})
	}

	id := c.nextID()
	if err := writePacket(c.conn, id, typeExecCmd, command); err != nil {
		return "", fmt.Errorf("rcon write: %w", err)
	}
	respID, _, payload, err := readPacket(c.conn)
	if err != nil {
		return "", fmt.Errorf("rcon read: %w", err)
	}
	if respID != id {
		return "", fmt.Errorf("rcon: response id mismatch (sent %d, got %d)", id, respID)
	}
	return payload, nil
}

// Close terminates the connection. Subsequent Cmd calls return ErrClosed.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	return c.conn.Close()
}

// Addr returns the host:port this client is connected to.
func (c *Client) Addr() string { return c.addr }

func (c *Client) nextID() int32 {
	id := c.id.Add(1)
	if id <= 0 {
		// IDs must be positive (negative is reserved for failed auth).
		c.id.Store(1)
		return 1
	}
	return id
}

func (c *Client) authenticate(password string) error {
	id := c.nextID()
	if err := writePacket(c.conn, id, typeLogin, password); err != nil {
		return fmt.Errorf("rcon auth write: %w", err)
	}
	respID, _, _, err := readPacket(c.conn)
	if err != nil {
		return fmt.Errorf("rcon auth read: %w", err)
	}
	// Server returns id = -1 on failed auth.
	if respID == -1 {
		return ErrAuthFailed
	}
	if respID != id {
		return fmt.Errorf("rcon: auth response id mismatch (sent %d, got %d)", id, respID)
	}
	return nil
}

// --- Wire helpers ---

func writePacket(w io.Writer, id, packetType int32, payload string) error {
	body := []byte(payload)
	// length = 4 (id) + 4 (type) + len(body) + 1 (null) + 1 (null pad)
	length := int32(4 + 4 + len(body) + 2)
	buf := make([]byte, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(id))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(packetType))
	copy(buf[12:], body)
	// last two bytes already zero (null term + null pad)
	_, err := w.Write(buf)
	return err
}

func readPacket(r io.Reader) (id, ptype int32, payload string, err error) {
	var lenBuf [4]byte
	if _, err = io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, 0, "", err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length < 10 || length > 4096 {
		return 0, 0, "", fmt.Errorf("rcon: implausible packet length %d", length)
	}
	body := make([]byte, length)
	if _, err = io.ReadFull(r, body); err != nil {
		return 0, 0, "", err
	}
	id = int32(binary.LittleEndian.Uint32(body[0:4]))
	ptype = int32(binary.LittleEndian.Uint32(body[4:8]))
	// Body has at least one null byte at the end; payload is up to the
	// first null after the 8-byte header.
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

// GenPassword returns a 32-byte random hex string suitable as an RCON
// password. Generated fresh per server start so a leaked password can't
// outlive the session.
func GenPassword() (string, error) {
	const n = 16
	b := make([]byte, n)
	_, err := readRand(b)
	if err != nil {
		return "", err
	}
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[2*i] = hex[v>>4]
		out[2*i+1] = hex[v&0x0F]
	}
	return string(out), nil
}
