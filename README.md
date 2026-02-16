# Podman Stratis Volume Plugin

A Podman volume plugin that provisions [Stratis](https://stratis-storage.github.io/) filesystems as container volumes.

Each volume gets its own XFS filesystem on a Stratis pool, providing:

- **Thin provisioning** - volumes only consume actual data size
- **Snapshots** - instant, space-efficient copies via XFS reflinks
- **Encryption** - LUKS integration via Stratis pool configuration
- **Optional size limits** - or unlimited thin-provisioned growth

## Requirements

- `podman` obviously, but it should with Docker too
- `stratisd` service running
- An existing Stratis pool

## Installation

Pre-built packages are available on the [GitHub Releases](https://github.com/kriansa/podman-volume-stratis/releases) page.

### RPM (RHEL/Fedora)

Download and install directly with `dnf`:

```bash
sudo dnf install https://github.com/kriansa/podman-volume-stratis/releases/latest/download/podman-volume-stratis_VERSION_linux_amd64.rpm
```

> Replace `VERSION` with the actual version number (e.g. `1.0.2`). Check the
> [Releases page](https://github.com/kriansa/podman-volume-stratis/releases) for
> the correct version and architecture (`amd64` or `arm64`).

The RPM installs config files, systemd units, and enables/starts the service automatically.

### From tarball (other Linux distros)

Download the tarball from [GitHub Releases](https://github.com/kriansa/podman-volume-stratis/releases), then install manually:

```bash
# Download and extract (replace VERSION and ARCH as needed)
curl -L https://github.com/kriansa/podman-volume-stratis/releases/latest/download/podman-volume-stratis_VERSION_linux_ARCH.tar.gz | tar xz

# Install binary
sudo cp podman-volume-stratis /usr/libexec/

# Install systemd service
sudo cp build/packaging/podman-volume-stratis.service /usr/lib/systemd/system/

# Install config files
sudo cp build/packaging/plugin-volume-stratis.conf /etc/containers/containers.conf.d/
sudo cp build/packaging/config.example.toml /etc/containers/plugin-volume-stratis.conf

# Enable and start the service
sudo systemctl daemon-reload
sudo systemctl enable --now podman-volume-stratis
```

### From source

```bash
# Compile
make compile

# Install binary
sudo cp build/dist/podman-volume-stratis /usr/libexec/

# Install service
sudo cp build/packaging/podman-volume-stratis.service /usr/lib/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now podman-volume-stratis
```

## Configuration

**RPM users:** Config files are installed automatically. Just edit
`/etc/containers/plugin-volume-stratis.conf` to set your pool name.

**Tarball/source users:** You must create the config files manually:

1. Create `/etc/containers/containers.conf.d/plugin-volume-stratis.conf` with the following content:

```toml
[engine.volume_plugins]
stratis = "/run/podman/plugins/volume-stratis.sock"
```

2. Create `/etc/containers/plugin-volume-stratis.conf` with the following content:

```toml
# Stratis pool name (required)
pool = "podman_vols"

# Mount path base directory
mount_path = "/mnt"

# Socket path
socket = "/run/podman/plugins/volume-stratis.sock"
```

## Usage

```bash
# Create a thin-provisioned volume (no size limit)
podman volume create --driver stratis myvolume

# Create a volume with size limit
podman volume create --driver stratis --opt size=10G myvolume

# Use in container
podman run -v myvolume:/data alpine

# Remove volume
podman volume rm myvolume
```

## Releasing

Releases are automated via [Release Please](https://github.com/googleapis/release-please).
Push conventional commits to `main` and Release Please opens a PR with the
version bump and changelog. Merging that PR creates a GitHub release and
triggers GoReleaser to build artifacts.

### Beta releases

To enter a beta release cycle:

```bash
make toggle-prerelease  # Enables pre-release mode
```

This flips the `prerelease` flag in `build/release-please-config.json`.
Subsequent release PRs will use beta versions (e.g., `1.1.0-beta.1`).

To exit the beta cycle:

```bash
make toggle-prerelease  # Disables pre-release mode
```

The next release PR will be a stable version.

## License

Apache 2.0
