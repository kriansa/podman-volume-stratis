package mount

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kriansa/podman-volume-stratis/internal/procmounts"
	"github.com/kriansa/podman-volume-stratis/internal/log"
)

// SyscallMounter implements Mounter using Linux syscalls
type SyscallMounter struct {
	basePath string // Base mount path for validation
}

// NewSyscallMounter creates a new syscall-based mounter
func NewSyscallMounter(basePath string) *SyscallMounter {
	return &SyscallMounter{
		basePath: basePath,
	}
}

// Mount mounts the source device to the target directory
func (m *SyscallMounter) Mount(source, target, fsType string) error {
	// Validate target is under base path
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}

	absBase, err := filepath.Abs(m.basePath)
	if err != nil {
		return fmt.Errorf("get absolute base path: %w", err)
	}

	if !strings.HasPrefix(absTarget, absBase+"/") && absTarget != absBase {
		return fmt.Errorf("mount target %q is not under base path %q", target, m.basePath)
	}

	log.Debug("mounting filesystem", "source", source, "target", target, "type", fsType)

	// Mount with no special flags
	if err := syscall.Mount(source, target, fsType, 0, ""); err != nil {
		return fmt.Errorf("mount %s to %s: %w", source, target, err)
	}

	log.Debug("mounted successfully", "source", source, "target", target)
	return nil
}

// Unmount unmounts the target directory
func (m *SyscallMounter) Unmount(target string) error {
	log.Debug("unmounting", "target", target)

	if err := syscall.Unmount(target, 0); err != nil {
		return fmt.Errorf("unmount %s: %w", target, err)
	}

	log.Debug("unmounted successfully", "target", target)
	return nil
}

// IsMounted checks if the target is mounted
func (m *SyscallMounter) IsMounted(target string) (bool, error) {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false, fmt.Errorf("get absolute path: %w", err)
	}

	mounts, err := procmounts.Parse()
	if err != nil {
		return false, fmt.Errorf("unable to parse mounts: %w", err)
	}

	for _, mount := range mounts {
		if mount.MountPoint == absTarget {
			return true, nil
		}
	}

	return false, nil
}

// GetMountPoint returns the mount point for a source device
func (m *SyscallMounter) GetMountPoint(source string) (string, error) {
	// Resolve source to absolute path
	absSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		// If we can't resolve, try with original path
		absSource = source
	}

	mounts, err := procmounts.Parse()
	if err != nil {
		return "", fmt.Errorf("unable to parse mounts: %w", err)
	}

	for _, mount := range mounts {
		// Check both the original path and resolved path
		if mount.Device == source || mount.Device == absSource {
			return mount.MountPoint, nil
		}
	}

	return "", nil
}
