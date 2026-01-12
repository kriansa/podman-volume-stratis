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

### From RPM (RHEL/Fedora)

```bash
# Build the RPM package (requires Docker)
make pkg-rhel

# Install
sudo dnf install build/dist/podman-volume-stratis-*.rpm

# Enable and start
sudo systemctl enable --now podman-volume-stratis
```

### From Source

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

1. Create `/etc/containers/containers.conf.d/plugin-volume-stratis.conf` with the following content:

```toml
[engine.volume_plugins]
stratis = "/run/podman/plugins/volume-stratis.sock"
```

2. Create `/etc/containers/plugin-volume-stratis.conf` with the following content

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

## License

Apache 2.0
