package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultConfigPath is the default location for the config file
	DefaultConfigPath = "/etc/containers/plugin-volume-stratis.conf"
	// DefaultSocketPath is the default Unix socket path for Podman
	DefaultSocketPath = "/run/podman/plugins/volume-stratis.sock"
	// DefaultMountPath is the default base directory for mounting volumes
	DefaultMountPath = "/mnt"
	// DefaultBackend is the default stratis backend
	DefaultBackend = "dbus"
)

// Config holds the plugin configuration
type Config struct {
	// Pool is the Stratis pool name to use for filesystems
	Pool string `toml:"pool"`
	// MountPath is the base directory for mounting volumes
	MountPath string `toml:"mount_path"`
	// SocketPath is the Unix socket path for the plugin
	SocketPath string `toml:"socket"`
	// Backend is the stratis backend to use: "dbus" or "cli"
	Backend string `toml:"backend"`
}

// Load loads configuration from a TOML file
// Returns an empty config if the file doesn't exist
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

// Merge merges CLI flags into the config, with CLI flags taking precedence
// over config file values. Empty CLI values are ignored.
func (c *Config) Merge(pool, mountPath, socketPath, backend string) {
	if pool != "" {
		c.Pool = pool
	}
	if mountPath != "" {
		c.MountPath = mountPath
	}
	if socketPath != "" {
		c.SocketPath = socketPath
	}
	if backend != "" {
		c.Backend = backend
	}
}

// ApplyDefaults applies default values for any unset fields
func (c *Config) ApplyDefaults() {
	if c.MountPath == "" {
		c.MountPath = DefaultMountPath
	}
	if c.SocketPath == "" {
		c.SocketPath = DefaultSocketPath
	}
	if c.Backend == "" {
		c.Backend = DefaultBackend
	}
}

// Validate validates the configuration
// Note: Pool existence is validated at runtime by the stratis manager
func (c *Config) Validate() error {
	if c.Pool == "" {
		return fmt.Errorf("pool name is required (use --pool or set 'pool' in config file)")
	}

	if c.Backend != "dbus" && c.Backend != "cli" {
		return fmt.Errorf("backend must be 'dbus' or 'cli', got %q", c.Backend)
	}

	return nil
}
