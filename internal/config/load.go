package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// envInterp matches ${VAR} or ${VAR:-default} for shell-style interpolation.
var envInterp = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)(?::-([^}]*))?\}`)

// Load reads and validates a config file. Environment variables in the form
// ${VAR} or ${VAR:-default} are interpolated before YAML parsing. Defaults
// are applied for unset fields. Returns the loaded Config and the absolute
// path of the file that was loaded.
func Load(path string) (*Config, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve config path: %w", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, abs, fmt.Errorf("read config %s: %w", abs, err)
	}
	expanded := expandEnv(raw)

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, abs, fmt.Errorf("parse config %s: %w", abs, err)
	}
	applyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, abs, err
	}
	return &cfg, abs, nil
}

func expandEnv(in []byte) []byte {
	return envInterp.ReplaceAllFunc(in, func(match []byte) []byte {
		groups := envInterp.FindSubmatch(match)
		key := string(groups[1])
		def := ""
		if len(groups) >= 3 {
			def = string(groups[2])
		}
		if v, ok := os.LookupEnv(key); ok {
			return []byte(v)
		}
		return []byte(def)
	})
}

func applyDefaults(c *Config) {
	if c.Instance.DataDir == "" {
		c.Instance.DataDir = "/var/lib/mcsm"
	}
	if c.API.Bind == "" {
		c.API.Bind = "0.0.0.0:8124"
	}
	if c.Discovery.ScanInterval == 0 {
		c.Discovery.ScanInterval = 60 * time.Second
	}
	if c.Peers.PingInterval == 0 {
		c.Peers.PingInterval = 30 * time.Second
	}
	if c.Peers.Timeout == 0 {
		c.Peers.Timeout = 5 * time.Second
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
	if c.Logging.Output == "" {
		c.Logging.Output = "stdout"
	}
	if c.Audit.Retention == 0 {
		c.Audit.Retention = 30 * 24 * time.Hour
	}
	if c.Metrics.Path == "" {
		c.Metrics.Path = "/metrics"
	}
	if c.API.CORS.MaxAge == 0 {
		c.API.CORS.MaxAge = 600 * time.Second
	}
	for i := range c.API.Tokens {
		t := &c.API.Tokens[i]
		if t.RateLimit == nil {
			t.RateLimit = &RateLimit{PerMinute: 600}
		}
	}
	if c.System.Temperature != nil && c.System.Temperature.Sensor == "" {
		c.System.Temperature.Sensor = "/sys/class/thermal/thermal_zone0/temp"
	}
}
