package config

import "time"

// Config is the root of config.yaml. It mirrors the schema documented in
// docs/api.md (Conventions §1) and docs/config.md (TODO).
type Config struct {
	Instance  Instance     `yaml:"instance"`
	API       API          `yaml:"api"`
	Discovery Discovery    `yaml:"discovery"`
	Slots     []Slot       `yaml:"slots"`
	Peers     PeersConfig  `yaml:"peers"`
	System    System       `yaml:"system"`
	Logging   Logging      `yaml:"logging"`
	Audit     Audit        `yaml:"audit"`
	Metrics   Metrics      `yaml:"metrics"`
}

type Instance struct {
	Name    string `yaml:"name"`
	DataDir string `yaml:"data_dir"`
}

type API struct {
	Bind        string       `yaml:"bind"`
	Tokens      []Token      `yaml:"tokens"`
	CORS        CORS         `yaml:"cors"`
	TLS         *TLS         `yaml:"tls"`
	PublicMeta  bool         `yaml:"public_meta"` // /version + /openapi.json without auth
}

// Token is configured with the argon2id hash, never the plaintext.
// Generate with `mcsm tokens new <scope...>` (TODO).
type Token struct {
	Name      string     `yaml:"name"`
	Hash      string     `yaml:"hash"`
	Scopes    []string   `yaml:"scopes"`
	RateLimit *RateLimit `yaml:"rate_limit"`
}

// RateLimit per-token. Zero PerMinute means unlimited.
type RateLimit struct {
	PerMinute int `yaml:"per_minute"`
}

type CORS struct {
	AllowedOrigins   []string      `yaml:"allowed_origins"`
	AllowCredentials bool          `yaml:"allow_credentials"`
	MaxAge           time.Duration `yaml:"max_age"`
}

type TLS struct {
	CertFile    string `yaml:"cert_file"`
	KeyFile     string `yaml:"key_file"`
	MinVersion  string `yaml:"min_version"`   // "1.2" or "1.3"; default "1.2"
	HSTSMaxAge  int    `yaml:"hsts_max_age"`  // seconds; 0 disables
}

type Discovery struct {
	Roots          []string      `yaml:"roots"`
	ScanInterval   time.Duration `yaml:"scan_interval"`
}

// Slot is a (name, port) pair that can host one server at a time.
type Slot struct {
	Name          string       `yaml:"name"`
	Port          int          `yaml:"port"`
	PublicAddress string       `yaml:"public_address"`
	AutoMount     string       `yaml:"auto_mount"` // server id (UUID) or empty
	Accepts       SlotAccepts  `yaml:"accepts"`
}

type SlotAccepts struct {
	Types         []string `yaml:"types"`           // empty = any
	MaxMemoryMB   int      `yaml:"max_memory_mb"`   // 0 = no cap
}

type PeersConfig struct {
	Peers        []Peer        `yaml:"peers"`
	PingInterval time.Duration `yaml:"ping_interval"`
	Timeout      time.Duration `yaml:"timeout"`
}

type Peer struct {
	Name  string `yaml:"name"`
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type System struct {
	Temperature *Temperature `yaml:"temperature"`
}

type Temperature struct {
	Sensor string `yaml:"sensor"`
	Policy string `yaml:"policy"` // path to temp-policy.yaml
}

type Logging struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // json | text
	Output string `yaml:"output"` // stdout | stderr | <path>
}

type Audit struct {
	Enabled   bool          `yaml:"enabled"`
	Retention time.Duration `yaml:"retention"`
}

type Metrics struct {
	Enabled     bool   `yaml:"enabled"`
	Path        string `yaml:"path"`
	RequireAuth bool   `yaml:"require_auth"`
}
