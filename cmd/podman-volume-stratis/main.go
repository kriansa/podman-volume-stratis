package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/urfave/cli/v3"

	"github.com/kriansa/podman-volume-stratis/internal/config"
	"github.com/kriansa/podman-volume-stratis/internal/driver"
	"github.com/kriansa/podman-volume-stratis/internal/mount"
	"github.com/kriansa/podman-volume-stratis/internal/stratis"
	"github.com/kriansa/podman-volume-stratis/internal/version"
	"github.com/kriansa/podman-volume-stratis/internal/log"
)

func main() {
	cmd := &cli.Command{
		Name:  "podman-volume-stratis",
		Usage: "A volume plugin that creates Stratis filesystems",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "pool",
				Aliases: []string{"p"},
				Usage:   "Stratis pool name",
			},
			&cli.StringFlag{
				Name:    "mount-path",
				Aliases: []string{"m"},
				Usage:   "Base directory for mounting volumes",
				Value:   config.DefaultMountPath,
			},
			&cli.StringFlag{
				Name:    "socket",
				Aliases: []string{"s"},
				Usage:   "Unix socket path for the plugin",
				Value:   config.DefaultSocketPath,
			},
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Configuration file path",
				Value:   config.DefaultConfigPath,
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Enable debug logging",
			},
			&cli.StringFlag{
				Name:    "backend",
				Aliases: []string{"b"},
				Usage:   "Stratis backend: dbus or cli",
				Value:   config.DefaultBackend,
			},
			&cli.BoolFlag{
				Name:    "version",
				Aliases: []string{"V"},
				Usage:   "Print version information",
			},
		},
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	// Handle version flag
	if cmd.Bool("version") {
		fmt.Println(version.String())
		return nil
	}

	// Setup logging
	log.Setup(cmd.Bool("verbose"))

	// Load config file
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Merge CLI flags (CLI takes precedence)
	cfg.Merge(
		cmd.String("pool"),
		cmd.String("mount-path"),
		cmd.String("socket"),
		cmd.String("backend"),
	)

	// Apply defaults
	cfg.ApplyDefaults()

	// Validate config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	log.Info("starting volume plugin",
		"pool", cfg.Pool,
		"mount_path", cfg.MountPath,
		"socket", cfg.SocketPath,
		"backend", cfg.Backend,
	)

	// Ensure mount path exists
	if err := os.MkdirAll(cfg.MountPath, 0755); err != nil {
		return fmt.Errorf("create mount path: %w", err)
	}

	// Create components
	stratisMgr, err := stratis.NewManager(cfg.Pool, cfg.Backend)
	if err != nil {
		return fmt.Errorf("create stratis manager: %w", err)
	}
	mounter := mount.NewSyscallMounter(cfg.MountPath)

	// Check pool exists
	poolExists, err := stratisMgr.PoolExists()
	if err != nil {
		return fmt.Errorf("check stratis pool: %w", err)
	}
	if !poolExists {
		return fmt.Errorf("stratis pool %q does not exist", cfg.Pool)
	}

	log.Debug("stratis pool verified", "pool", cfg.Pool)

	// Create driver
	d := driver.NewDriver(
		cfg.MountPath,
		stratisMgr,
		mounter,
	)

	// Create handler
	h := volume.NewHandler(d)

	// Ensure socket directory exists
	socketDir := filepath.Dir(cfg.SocketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Remove existing socket if present (stale from previous run)
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing socket: %w", err)
	}

	// Clean up socket on exit
	defer func() {
		if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
			log.Warn("failed to remove socket on shutdown", "path", cfg.SocketPath, "error", err)
		}
	}()

	log.Info("listening on socket", "path", cfg.SocketPath)
	return h.ServeUnix(cfg.SocketPath, 0)
}
