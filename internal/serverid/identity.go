package serverid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MertDalbudak/mcsm/internal/ids"
	"gopkg.in/yaml.v3"
)

// ConfigDir is the per-server directory holding mcsm's metadata.
// Named ".mcsm" so it doesn't collide with Bukkit/Paper's "plugins/mcsm/"
// (which doesn't exist on Vanilla/Fabric anyway).
const ConfigDir = ".mcsm"

// ConfigFile is the YAML inside ConfigDir.
const ConfigFile = "config.yaml"

// ErrNotAServer is returned when a directory has no .mcsm/config.yaml and
// no recognizable Minecraft artifacts. Callers use it to skip non-server
// subdirectories during discovery without treating them as failures.
var ErrNotAServer = errors.New("not a minecraft server directory")

// Config is the per-server identity file at <server-dir>/.mcsm/config.yaml.
// Phase 2A only needs ID/Name/Type. The remaining fields are included so
// the schema doesn't shift later — they're populated as features land.
type Config struct {
	ID       string         `yaml:"id"`
	Name     string         `yaml:"name"`
	Type     string         `yaml:"type"` // paper | vanilla | fabric | forge
	Java     JavaConfig     `yaml:"java,omitempty"`
	RCON     RCONConfig     `yaml:"rcon,omitempty"`
	Features map[string]any `yaml:"features,omitempty"`
	Discord  DiscordConfig  `yaml:"discord,omitempty"`
	Restart  RestartConfig  `yaml:"restart,omitempty"`
}

type JavaConfig struct {
	Args []string `yaml:"args,omitempty"`
}

type RCONConfig struct {
	Port     int    `yaml:"port,omitempty"`
	Password string `yaml:"password,omitempty"`
}

type DiscordConfig struct {
	Token    string   `yaml:"token,omitempty"`
	Channels []string `yaml:"channels,omitempty"`
}

type RestartConfig struct {
	Cron             string `yaml:"cron,omitempty"`
	GracefulSeconds  int    `yaml:"graceful_seconds,omitempty"`
}

// ConfigPath returns the absolute path of a server's .mcsm/config.yaml.
func ConfigPath(serverDir string) string {
	return filepath.Join(serverDir, ConfigDir, ConfigFile)
}

// Read loads a server's config. Returns ErrNotAServer wrapped if the file
// doesn't exist, so callers can distinguish "not a server" from a real
// IO/parse error.
func Read(serverDir string) (*Config, error) {
	p := ConfigPath(serverDir)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotAServer, serverDir)
		}
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if c.ID == "" {
		return nil, fmt.Errorf("%s: id field is required", p)
	}
	return &c, nil
}

// Write atomically persists a server's config (write-temp + rename).
// Creates .mcsm/ if needed.
func Write(serverDir string, c *Config) error {
	dir := filepath.Join(serverDir, ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	body, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	final := ConfigPath(serverDir)
	tmp, err := os.CreateTemp(dir, ".config.yaml.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, final); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Initialize seeds a new .mcsm/config.yaml on a server directory that
// doesn't have one yet. Generates a UUIDv7 id and detects the flavor.
// If a config already exists it returns the existing one untouched.
func Initialize(serverDir, name string) (*Config, error) {
	if c, err := Read(serverDir); err == nil {
		return c, nil
	} else if !errors.Is(err, ErrNotAServer) {
		return nil, err
	}
	flavor, err := DetectFlavor(serverDir)
	if err != nil {
		return nil, err
	}
	c := &Config{
		ID:   ids.NewServerID(),
		Name: name,
		Type: flavor,
	}
	if err := Write(serverDir, c); err != nil {
		return nil, err
	}
	return c, nil
}
