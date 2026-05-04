// Package peers manages connections to other MCSM instances. The pool
// pings every configured peer on an interval; federation handlers use
// the pool's snapshot to fan out read requests across reachable peers.
package peers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an HTTP client for one peer URL. Stateless aside from the
// embedded http.Client.
type Client struct {
	name  string
	url   string // base, no trailing slash
	token string
	http  *http.Client
}

// NewClient constructs a peer client with sane timeouts. The pool calls
// this once per configured peer at startup.
func NewClient(name, url, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		name:  name,
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: timeout},
	}
}

func (c *Client) Name() string { return c.name }
func (c *Client) URL() string  { return c.url }

// get is a small JSON-decoding helper. status is returned even on
// success so callers can record it for telemetry.
func (c *Client) get(ctx context.Context, path string, dst any) (status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // cap 4MB
	if resp.StatusCode >= 400 {
		// Try to surface the documented error envelope.
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if jerr := json.Unmarshal(body, &e); jerr == nil && e.Error.Code != "" {
			return resp.StatusCode, fmt.Errorf("peer %s %s: %s (%s)", c.name, path, e.Error.Code, e.Error.Message)
		}
		return resp.StatusCode, fmt.Errorf("peer %s %s: HTTP %d", c.name, path, resp.StatusCode)
	}
	if dst == nil {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return resp.StatusCode, fmt.Errorf("peer %s %s: parse: %w", c.name, path, err)
	}
	return resp.StatusCode, nil
}

// PingResult comes from /api/v1/instance.
type PingResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Ping fetches the peer's identity. Used as the reachability check.
func (c *Client) Ping(ctx context.Context) (*PingResult, error) {
	var r PingResult
	if _, err := c.get(ctx, "/api/v1/instance", &r); err != nil {
		return nil, err
	}
	if r.Name == "" {
		return nil, errors.New("peer returned empty instance name")
	}
	return &r, nil
}

// Discovery proxies the peer's /api/v1/discovery into a generic map so
// federation can pass fields through without re-typing them all here.
func (c *Client) Discovery(ctx context.Context) (map[string]any, error) {
	var r map[string]any
	if _, err := c.get(ctx, "/api/v1/discovery", &r); err != nil {
		return nil, err
	}
	return r, nil
}

// Slots proxies the peer's /api/v1/slots.
func (c *Client) Slots(ctx context.Context) (map[string]any, error) {
	var r map[string]any
	if _, err := c.get(ctx, "/api/v1/slots", &r); err != nil {
		return nil, err
	}
	return r, nil
}
