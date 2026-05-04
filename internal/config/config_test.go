package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validHash = "$argon2id$v=19$m=65536,t=3,p=2$CSBfFPPsdwofQbQkQkjN1w$iKjWVSlQw1yhHG5E9cRC2/8d00nBb3+PUScUIRiepoM"

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_Defaults(t *testing.T) {
	body := `instance:
  name: node-a
api:
  bind: 127.0.0.1:8124
  tokens:
    - name: t1
      hash: "` + validHash + `"
      scopes: ["*"]
discovery:
  roots: [/tmp/x]
slots:
  - name: s1
    port: 25565`
	p := writeTemp(t, body)
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Instance.DataDir != "/var/lib/mcsm" {
		t.Errorf("DataDir default: got %q", cfg.Instance.DataDir)
	}
	if cfg.Discovery.ScanInterval != 60*time.Second {
		t.Errorf("ScanInterval default: %v", cfg.Discovery.ScanInterval)
	}
	if cfg.Logging.Level != "info" || cfg.Logging.Format != "json" || cfg.Logging.Output != "stdout" {
		t.Errorf("Logging defaults wrong: %+v", cfg.Logging)
	}
	if cfg.Audit.Retention != 30*24*time.Hour {
		t.Errorf("Audit.Retention default: %v", cfg.Audit.Retention)
	}
	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("Metrics.Path default: %q", cfg.Metrics.Path)
	}
	if cfg.API.Tokens[0].RateLimit == nil || cfg.API.Tokens[0].RateLimit.PerMinute != 600 {
		t.Errorf("rate limit default: %+v", cfg.API.Tokens[0].RateLimit)
	}
}

func TestLoad_EnvInterpolation(t *testing.T) {
	t.Setenv("INSTANCE_NAME", "node-from-env")
	t.Setenv("UNUSED_VAR", "ignored")
	body := `instance:
  name: ${INSTANCE_NAME}
  data_dir: ${MISSING_VAR:-/var/lib/fallback}
api:
  bind: 127.0.0.1:8124
  tokens:
    - name: t1
      hash: "` + validHash + `"
      scopes: ["*"]
discovery:
  roots: [/tmp/x]
slots:
  - name: s1
    port: 25565`
	p := writeTemp(t, body)
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Instance.Name != "node-from-env" {
		t.Errorf("env interp: got %q", cfg.Instance.Name)
	}
	if cfg.Instance.DataDir != "/var/lib/fallback" {
		t.Errorf("default fallback: got %q", cfg.Instance.DataDir)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "good",
			mutate:  func(c *Config) {},
			wantErr: "",
		},
		{
			name:    "instance_name_invalid",
			mutate:  func(c *Config) { c.Instance.Name = "Bad Name" },
			wantErr: "instance.name",
		},
		{
			name:    "bind_invalid",
			mutate:  func(c *Config) { c.API.Bind = "not-a-host-port" },
			wantErr: "api.bind",
		},
		{
			name:    "no_tokens",
			mutate:  func(c *Config) { c.API.Tokens = nil },
			wantErr: "api.tokens",
		},
		{
			name: "duplicate_token_names",
			mutate: func(c *Config) {
				c.API.Tokens = append(c.API.Tokens, Token{
					Name: c.API.Tokens[0].Name, Hash: validHash, Scopes: []string{"*"},
				})
			},
			wantErr: "duplicate name",
		},
		{
			name:    "bad_hash",
			mutate:  func(c *Config) { c.API.Tokens[0].Hash = "plaintext-uh-oh" },
			wantErr: "argon2id",
		},
		{
			name:    "no_scopes",
			mutate:  func(c *Config) { c.API.Tokens[0].Scopes = nil },
			wantErr: "scope required",
		},
		{
			name:    "empty_discovery_roots",
			mutate:  func(c *Config) { c.Discovery.Roots = nil },
			wantErr: "discovery.roots",
		},
		{
			name:    "empty_slots",
			mutate:  func(c *Config) { c.Slots = nil },
			wantErr: "slots must contain",
		},
		{
			name:    "duplicate_slot_names",
			mutate: func(c *Config) {
				c.Slots = []Slot{{Name: "x", Port: 25565}, {Name: "x", Port: 25566}}
			},
			wantErr: "duplicate name",
		},
		{
			name:    "duplicate_slot_ports",
			mutate: func(c *Config) {
				c.Slots = []Slot{{Name: "a", Port: 25565}, {Name: "b", Port: 25565}}
			},
			wantErr: "already used by slot",
		},
		{
			name:    "bad_log_level",
			mutate:  func(c *Config) { c.Logging.Level = "trace" },
			wantErr: "logging.level",
		},
		{
			name:    "tls_missing_cert",
			mutate: func(c *Config) {
				c.API.TLS = &TLS{KeyFile: "/x/key.pem"}
			},
			wantErr: "cert_file",
		},
		{
			name:    "self_peer",
			mutate: func(c *Config) {
				c.Peers.Peers = []Peer{{Name: "node-a", URL: "https://x:8124", Token: "t"}}
			},
			wantErr: "cannot peer with self",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{
				Instance: Instance{Name: "node-a"},
				API: API{
					Bind: "127.0.0.1:8124",
					Tokens: []Token{{
						Name: "tok", Hash: validHash, Scopes: []string{"*"},
					}},
				},
				Discovery: Discovery{Roots: []string{"/tmp"}},
				Slots:     []Slot{{Name: "s1", Port: 25565}},
				Logging:   Logging{Level: "info", Format: "json"},
			}
			tc.mutate(c)
			err := Validate(c)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected ok, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}
