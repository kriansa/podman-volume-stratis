# Build System

This project uses **GoReleaser** as the single source of truth for builds, with **Release Please** for automated versioning and changelog management.

## Quick Reference

```bash
make build      # Build for current platform
make build-all  # Build for all platforms (amd64, arm64)
make build-x86  # Build x86_64 only (for integration tests)
make pkg        # Build binaries + RPM packages
make clean      # Clean build artifacts
```

## Directory Structure

```
build/
├── dist/                          # Build output (gitignored)
├── goreleaser.yml                 # Build configuration (single source of truth)
├── packaging/                     # Files included in packages
│   ├── config.example.toml
│   ├── plugin-volume-stratis.conf
│   ├── podman-volume-stratis.service
│   └── scripts/
│       ├── postinstall.sh
│       └── preremove.sh
├── release-please-config.json     # Release Please settings
└── release-please-manifest.json   # Current version tracking
```

## Local Development

All builds run via Docker (no local GoReleaser installation needed):

```bash
# Development build (current platform, fast)
make build
./build/dist/podman-volume-stratis_linux_amd64_v1/podman-volume-stratis --version

# Test full release locally (all platforms + packages, no upload)
make pkg
ls build/dist/*.rpm build/dist/*.tar.gz
```

### Snapshot Versions

Local builds use snapshot versioning: `0.0.0-SNAPSHOT-<commit>`

## Release Workflow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                 DEVELOPMENT                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Developer pushes to main                                                  │
│         │                                                                   │
│         ▼                                                                   │
│   ┌─────────────────┐                                                       │
│   │ Release Please  │ ──► Analyzes commits (feat:, fix:, etc.)              │
│   └────────┬────────┘                                                       │
│            │                                                                │
│            ▼                                                                │
│   ┌─────────────────┐                                                       │
│   │  Release PR     │ ◄── Accumulates changes, updates CHANGELOG.md         │
│   │  (auto-updated) │                                                       │
│   └─────────────────┘                                                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                                  RELEASE                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Maintainer merges Release PR                                              │
│         │                                                                   │
│         ▼                                                                   │
│   ┌─────────────────┐                                                       │
│   │ Release Please  │ ──► Creates git tag (v1.2.3)                          │
│   └────────┬────────┘     Creates GitHub Release with changelog             │
│            │                                                                │
│            ▼                                                                │
│   ┌─────────────────┐     ┌──────────────────────────────────────┐          │
│   │   GoReleaser    │ ──► │ Artifacts:                           │          │
│   └─────────────────┘     │  • podman-volume-stratis_linux_amd64 │          │
│                           │  • podman-volume-stratis_linux_arm64 │          │
│                           │  • podman-volume-stratis-*.rpm (x2)  │          │
│                           │  • checksums.txt                     │          │
│                           └──────────────────────────────────────┘          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Versioning

| Context | Version Format | Source |
|---------|---------------|--------|
| Local builds | `0.0.0-SNAPSHOT-<commit>` | GoReleaser snapshot |
| Releases | `X.Y.Z` | Git tag from Release Please |

### Conventional Commits

Release Please determines version bumps from commit prefixes:

| Commit Prefix | Version Bump | Example |
|---------------|--------------|---------|
| `feat:` | Minor (0.X.0) | `feat: add quota support` |
| `fix:` | Patch (0.0.X) | `fix: handle empty pool` |
| `feat!:` or `BREAKING CHANGE:` | Major (X.0.0) | `feat!: new config format` |

## Release Artifacts

Each release includes:

| Artifact | Description |
|----------|-------------|
| `podman-volume-stratis_X.Y.Z_linux_amd64.tar.gz` | Binary + docs (x86_64) |
| `podman-volume-stratis_X.Y.Z_linux_arm64.tar.gz` | Binary + docs (ARM64) |
| `podman-volume-stratis-X.Y.Z-1.x86_64.rpm` | RPM package (x86_64) |
| `podman-volume-stratis-X.Y.Z-1.aarch64.rpm` | RPM package (ARM64) |
| `checksums.txt` | SHA256 checksums |

## Configuration Files

### `goreleaser.yml`

Single source of truth for:
- Build settings (CGO_ENABLED=0, ldflags)
- Target platforms (linux/amd64, linux/arm64)
- Archive contents
- RPM package configuration (replaces old nfpm config)

### `release-please-config.json`

Release Please settings:
- Release type: `go`
- Changelog path: `CHANGELOG.md`

### `release-please-manifest.json`

Tracks current version. Updated automatically by Release Please.

## CI/CD Workflows

```
.github/workflows/
├── release-please.yml   # Runs on push to main
│                        # Creates/updates Release PR
│                        # Triggers release.yml on merge
│
└── release.yml          # Runs GoReleaser on new tags
                         # Also supports manual dispatch
```

## Manual Release (if needed)

Re-run a failed release manually via GitHub Actions:
- Go to Actions → Release → Run workflow → Enter tag (e.g., v1.0.0)
