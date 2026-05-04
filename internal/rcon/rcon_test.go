package rcon

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// stubServer is a minimal RCON listener used to drive Client tests.
// It mimics the protocol just enough to authenticate and echo commands.
type stubServer struct {
	listener net.Listener
	password string
}

func newStub(t *testing.T, password string) *stubServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &stubServer{listener: ln, password: password}
	t.Cleanup(func() { ln.Close() })
	go s.accept()
	return s
}

func (s *stubServer) addr() (host string, port int) {
	a := s.listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (s *stubServer) accept() {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *stubServer) serve(c net.Conn) {
	defer c.Close()
	authed := false
	for {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		id, ptype, payload, err := readPkt(c)
		if err != nil {
			return
		}
		switch ptype {
		case typeLogin:
			if payload == s.password {
				authed = true
				_ = writePkt(c, id, typeAuthResp, "")
			} else {
				_ = writePkt(c, -1, typeAuthResp, "")
				return
			}
		case typeExecCmd:
			if !authed {
				return
			}
			resp := "echoed: " + payload
			if payload == "stop" {
				resp = "Stopping the server"
			}
			_ = writePkt(c, id, typeResponse, resp)
		}
	}
}

func readPkt(r io.Reader) (id, ptype int32, payload string, err error) {
	var lb [4]byte
	if _, err = io.ReadFull(r, lb[:]); err != nil {
		return
	}
	length := binary.LittleEndian.Uint32(lb[:])
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

func writePkt(w io.Writer, id, ptype int32, payload string) error {
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

func TestDial_AuthSuccess(t *testing.T) {
	s := newStub(t, "letmein")
	host, port := s.addr()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, host, port, "letmein")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
}

func TestDial_AuthFailure(t *testing.T) {
	s := newStub(t, "secret")
	host, port := s.addr()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, host, port, "wrong")
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestCmd_RoundTrip(t *testing.T) {
	s := newStub(t, "p")
	host, port := s.addr()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, host, port, "p")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.Cmd(context.Background(), "list")
	if err != nil {
		t.Fatalf("Cmd: %v", err)
	}
	if resp != "echoed: list" {
		t.Errorf("unexpected response: %q", resp)
	}
}

func TestCmd_ManyConcurrent_Serializes(t *testing.T) {
	s := newStub(t, "p")
	host, port := s.addr()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, host, port, "p")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const N = 20
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := c.Cmd(context.Background(), "ping")
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Errorf("concurrent Cmd #%d: %v", i, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for concurrent commands")
		}
	}
}

func TestCmd_OnClosedClient(t *testing.T) {
	s := newStub(t, "p")
	host, port := s.addr()
	c, err := Dial(context.Background(), host, port, "p")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	_, err = c.Cmd(context.Background(), "list")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestCmd_OversizedRejectedClientSide(t *testing.T) {
	s := newStub(t, "p")
	host, port := s.addr()
	c, err := Dial(context.Background(), host, port, "p")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	big := strings.Repeat("x", maxPayload+1)
	_, err = c.Cmd(context.Background(), big)
	if err == nil {
		t.Error("expected rejection of oversized command")
	}
}

func TestGenPassword_Format(t *testing.T) {
	p, err := GenPassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 32 {
		t.Errorf("password length: got %d want 32", len(p))
	}
	for _, r := range p {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			t.Errorf("non-hex char in password: %q", p)
			break
		}
	}
}

func TestGenPassword_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p, err := GenPassword()
		if err != nil {
			t.Fatal(err)
		}
		if seen[p] {
			t.Fatalf("collision in %d iterations", i)
		}
		seen[p] = true
	}
}
