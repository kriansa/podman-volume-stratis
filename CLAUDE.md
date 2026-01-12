# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Docker/Podman volume plugin that creates Stratis filesystems for each volume on a Stratis pool. Implements the Docker Volume Plugin protocol via Unix socket.

**Key Features:**
- Thin-provisioned volumes by default (optional size limits)
- Two backends: D-Bus (default) or CLI
- XFS filesystems (Stratis requirement)
- Thread-safe operations via mutex
- Human-friendly and structured logging

## Build & Test Commands

```bash
# Build
make clean-build build

# Run unit tests
make test-unit

# Run single unit test
go test ./internal/validation -run TestValidateVolumeName

# Run integration tests (requires QEMU VM image)
make test-integration

# Build VM image for integration tests (requires Packer)
make test-integration-image

# Run linter
golangci-lint run ./...
```

**Build output:** `build/dist/podman-volume-stratis`

## Architecture

The plugin implements the Docker Volume Plugin protocol using `github.com/docker/go-plugins-helpers/volume`.

### Package Structure

- **cmd/podman-volume-stratis/** - CLI entrypoint using urfave/cli/v3
- **internal/driver/** - Volume driver implementing `volume.Driver` interface with mutex-protected operations (Create, Remove, Mount, Unmount, Path, Get, List, Capabilities)
- **internal/stratis/** - Stratis filesystem management via `Manager` interface with two backends:
  - `CLIManager` - Shells out to `stratis` CLI command
  - `DBusManager` - Direct D-Bus communication with stratisd (`org.storage.stratis3`)
- **internal/mount/** - Mount/unmount via syscalls (`Mounter` interface)
- **internal/config/** - TOML config parsing and validation
- **internal/validation/** - Volume name validation (Docker naming rules)
- **internal/procmounts/** - `/proc/mounts` parser
- **internal/version/** - Version info injected via ldflags at build time
- **internal/log/** - slog wrapper for structured logging

### Key Design Patterns

- All mutation operations (create, delete, mount, unmount) protected by mutex in driver
- Each subsystem has an interface for testability (Manager, Mounter, DBusConnection)
- Errors wrapped with context: `fmt.Errorf("operation: %w", err)`
- Sentinel error: `stratis.ErrNotFound` for missing filesystems
- Volume names map 1:1 to Stratis filesystem names
- D-Bus pool path cached and invalidated on mutations
- Mount paths validated to be under configured base path (security)
- Graceful degradation: non-fatal errors don't crash operations

### External Dependencies

- `stratisd` - Stratis storage daemon (D-Bus backend)
- `stratis` - Stratis CLI (CLI backend)

## Configuration

**Config file:** `/etc/containers/plugin-volume-stratis.conf` (TOML)

```toml
pool = "podman_vols"           # Required: Stratis pool name
mount_path = "/mnt"            # Base directory for volume mounts
socket = "/run/podman/plugins/volume-stratis.sock"
backend = "dbus"               # "dbus" (default) or "cli"
```

**CLI flags** (override config file):
- `--pool, -p` - Stratis pool name (required)
- `--mount-path, -m` - Base mount directory (default: `/mnt`)
- `--socket, -s` - Unix socket path
- `--config, -c` - Config file path
- `--backend, -b` - Backend: "dbus" or "cli"
- `--verbose, -v` - Enable debug logging
- `--version, -V` - Print version info

**Defaults:**
- Socket: `/run/podman/plugins/volume-stratis.sock`
- Mount path: `/mnt`
- Backend: `dbus`

## Volume Name Validation

Docker naming rules enforced (see `internal/validation/`):
- **Length:** 2-65 characters
- **Pattern:** `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`
- Must start with alphanumeric
- Allowed characters: alphanumeric, underscore, dot, hyphen

## Size Options

Volumes support optional size limits via `--opt size=<value>`:
- IEC units: `KiB`, `MiB`, `GiB`, `TiB` (powers of 1024)
- SI units: `KB`, `MB`, `GB`, `TB` (powers of 1000)
- Without size: thin-provisioned (default)

## Error Handling

```go
// Always wrap errors with context
return fmt.Errorf("operation: %w", err)

// Check sentinel errors
if errors.Is(err, stratis.ErrNotFound) {
    return fmt.Errorf("volume %s not found", name)
}
```

## Logging

Use the `internal/log` package. Always log in lowercase:
```go
log.Debug("creating filesystem", "name", fsName)
log.Info("volume created", "name", name, "size", size)
log.Warn("failed to remove socket", "path", path, "error", err)
```

## Integration Testing

Integration tests run in a QEMU VM with real Stratis:

**Location:** `tests/integration/` (build tag: `//go:build integration`)

**Infrastructure:**
- `tests/integration/vm/` - QEMU VM management (SSH communication)
- `tests/integration/driverclient/` - Plugin client over Unix socket
- `tests/packer/` - Packer config for Fedora VM image

**Test constants:**
- Pool: `test_pool`
- Socket: `/run/podman/plugins/volume-stratis.sock`
- Mount base: `/mnt/volumes`

**VM credentials:** `fedora` / `fedora`

## Important Paths

**Runtime:**
- Config: `/etc/containers/plugin-volume-stratis.conf`
- Socket: `/run/podman/plugins/volume-stratis.sock`
- Mounts: `/mnt/<volume-name>` (configurable)
- Devices: `/dev/stratis/<pool>/<volume-name>`

**Build:**
- Binary: `build/dist/podman-volume-stratis`
- VM image: `tests/images/fedora-stratis.qcow2`
