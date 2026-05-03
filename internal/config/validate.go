package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	instanceNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	slotNameRe     = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	peerNameRe     = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
)

// Validate enforces invariants the rest of the program relies on.
// Errors are joined so the operator sees every problem at once, not just
// the first.
func Validate(c *Config) error {
	var errs []error

	if !instanceNameRe.MatchString(c.Instance.Name) {
		errs = append(errs, fmt.Errorf("instance.name %q must match %s", c.Instance.Name, instanceNameRe))
	}

	if _, _, err := net.SplitHostPort(c.API.Bind); err != nil {
		errs = append(errs, fmt.Errorf("api.bind %q: %w", c.API.Bind, err))
	}

	if c.API.TLS != nil {
		if c.API.TLS.CertFile == "" || c.API.TLS.KeyFile == "" {
			errs = append(errs, errors.New("api.tls requires both cert_file and key_file"))
		}
		if v := c.API.TLS.MinVersion; v != "" && v != "1.2" && v != "1.3" {
			errs = append(errs, fmt.Errorf("api.tls.min_version must be \"1.2\" or \"1.3\", got %q", v))
		}
	}

	if len(c.API.Tokens) == 0 {
		errs = append(errs, errors.New("api.tokens must contain at least one token"))
	}
	tokenNames := map[string]bool{}
	for i, t := range c.API.Tokens {
		if t.Name == "" {
			errs = append(errs, fmt.Errorf("api.tokens[%d].name is required", i))
		}
		if tokenNames[t.Name] {
			errs = append(errs, fmt.Errorf("api.tokens[%d]: duplicate name %q", i, t.Name))
		}
		tokenNames[t.Name] = true
		if !strings.HasPrefix(t.Hash, "$argon2id$") {
			errs = append(errs, fmt.Errorf("api.tokens[%d] %q: hash must be argon2id (starts with $argon2id$)", i, t.Name))
		}
		if len(t.Scopes) == 0 {
			errs = append(errs, fmt.Errorf("api.tokens[%d] %q: at least one scope required", i, t.Name))
		}
	}

	if len(c.Discovery.Roots) == 0 {
		errs = append(errs, errors.New("discovery.roots must contain at least one path"))
	}

	if len(c.Slots) == 0 {
		errs = append(errs, errors.New("slots must contain at least one slot"))
	}
	slotNames := map[string]bool{}
	slotPorts := map[int]string{}
	for i, s := range c.Slots {
		if !slotNameRe.MatchString(s.Name) {
			errs = append(errs, fmt.Errorf("slots[%d].name %q must match %s", i, s.Name, slotNameRe))
		}
		if slotNames[s.Name] {
			errs = append(errs, fmt.Errorf("slots[%d]: duplicate name %q", i, s.Name))
		}
		slotNames[s.Name] = true
		if s.Port < 1 || s.Port > 65535 {
			errs = append(errs, fmt.Errorf("slots[%d] %q: port %d out of range", i, s.Name, s.Port))
		}
		if other, dup := slotPorts[s.Port]; dup {
			errs = append(errs, fmt.Errorf("slots[%d] %q: port %d already used by slot %q", i, s.Name, s.Port, other))
		}
		slotPorts[s.Port] = s.Name
	}

	peerNames := map[string]bool{}
	for i, p := range c.Peers.Peers {
		if !peerNameRe.MatchString(p.Name) {
			errs = append(errs, fmt.Errorf("peers[%d].name %q must match %s", i, p.Name, peerNameRe))
		}
		if peerNames[p.Name] {
			errs = append(errs, fmt.Errorf("peers[%d]: duplicate name %q", i, p.Name))
		}
		peerNames[p.Name] = true
		if p.Name == c.Instance.Name {
			errs = append(errs, fmt.Errorf("peers[%d] %q: cannot peer with self (matches instance.name)", i, p.Name))
		}
		u, err := url.Parse(p.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("peers[%d] %q: invalid url %q", i, p.Name, p.URL))
		}
		if p.Token == "" {
			errs = append(errs, fmt.Errorf("peers[%d] %q: token is required", i, p.Name))
		}
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level %q must be one of debug|info|warn|error", c.Logging.Level))
	}
	switch c.Logging.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("logging.format %q must be json or text", c.Logging.Format))
	}

	return errors.Join(errs...)
}
