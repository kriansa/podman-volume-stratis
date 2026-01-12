package driver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/kriansa/podman-volume-stratis/internal/mount"
	"github.com/kriansa/podman-volume-stratis/internal/stratis"
	"github.com/kriansa/podman-volume-stratis/internal/validation"
	"github.com/kriansa/podman-volume-stratis/internal/log"
)

// Driver implements the Docker volume plugin interface
type Driver struct {
	mu        sync.Mutex
	mountPath string
	stratis   stratis.Manager
	mounter   mount.Mounter
}

// NewDriver creates a new volume driver
func NewDriver(
	mountPath string,
	stratisMgr stratis.Manager,
	mounter mount.Mounter,
) *Driver {
	return &Driver{
		mountPath: mountPath,
		stratis:   stratisMgr,
		mounter:   mounter,
	}
}

// Create creates a new volume
func (d *Driver) Create(req *volume.CreateRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	log.Debug("creating volume", "name", req.Name, "options", req.Options)

	// 1. Validate name
	if err := validation.ValidateVolumeName(req.Name); err != nil {
		return err
	}

	// 2. Parse size from options (optional for Stratis - thin provisioning)
	var sizeLimit *uint64
	if sizeStr := req.Options["size"]; sizeStr != "" {
		size, err := parseSize(sizeStr)
		if err != nil {
			return fmt.Errorf("invalid size %q: %w", sizeStr, err)
		}
		sizeLimit = &size
	}

	// 3. Check uniqueness
	if fs, err := d.stratis.GetByName(req.Name); err == nil && fs != nil {
		return fmt.Errorf("volume %s already exists", req.Name)
	} else if err != nil && !errors.Is(err, stratis.ErrNotFound) {
		return fmt.Errorf("check existing volume: %w", err)
	}

	// 4. Create filesystem
	fs, err := d.stratis.Create(req.Name, sizeLimit)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	if sizeLimit != nil {
		log.Info("volume created", "name", req.Name, "sizeLimit", *sizeLimit)
	} else {
		log.Info("volume created (thin provisioned)", "name", req.Name, "device", fs.DevicePath)
	}
	return nil
}

// Remove removes a volume
func (d *Driver) Remove(req *volume.RemoveRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	log.Debug("removing volume", "name", req.Name)

	// Check if filesystem exists
	fs, err := d.stratis.GetByName(req.Name)
	if err != nil {
		if errors.Is(err, stratis.ErrNotFound) {
			return fmt.Errorf("volume %s not found", req.Name)
		}
		return fmt.Errorf("get volume: %w", err)
	}

	// Check if mounted and unmount if necessary
	mountPoint := d.mountPointPath(req.Name)
	mounted, err := d.mounter.IsMounted(mountPoint)
	if err != nil {
		return fmt.Errorf("check mount status: %w", err)
	}

	if mounted {
		if err := d.mounter.Unmount(mountPoint); err != nil {
			return fmt.Errorf("unmount: %w", err)
		}
	}

	// Remove mount directory if it exists
	if err := os.RemoveAll(mountPoint); err != nil && !os.IsNotExist(err) {
		log.Warn("failed to remove mount directory", "path", mountPoint, "error", err)
	}

	// Delete filesystem
	if err := d.stratis.Delete(fs.Name); err != nil {
		return fmt.Errorf("delete filesystem: %w", err)
	}

	log.Info("volume removed", "name", req.Name)
	return nil
}

// Mount mounts a volume
func (d *Driver) Mount(req *volume.MountRequest) (*volume.MountResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	log.Debug("mounting volume", "name", req.Name, "id", req.ID)

	// Check if filesystem exists
	fs, err := d.stratis.GetByName(req.Name)
	if err != nil {
		if errors.Is(err, stratis.ErrNotFound) {
			return nil, fmt.Errorf("volume %s not found", req.Name)
		}
		return nil, fmt.Errorf("get volume: %w", err)
	}

	mountPoint := d.mountPointPath(req.Name)

	// Check if already mounted
	existingMount, err := d.mounter.GetMountPoint(fs.DevicePath)
	if err != nil {
		return nil, fmt.Errorf("check existing mount: %w", err)
	}

	if existingMount != "" {
		// Already mounted somewhere
		if existingMount == mountPoint {
			// Mounted at correct path
			log.Debug("volume already mounted", "name", req.Name, "path", mountPoint)
			return &volume.MountResponse{Mountpoint: mountPoint}, nil
		}
		// Mounted elsewhere
		return nil, fmt.Errorf("volume %s is mounted at %s instead of %s", req.Name, existingMount, mountPoint)
	}

	// Create mount directory
	if err := d.prepareMountPoint(mountPoint); err != nil {
		return nil, fmt.Errorf("prepare mount point: %w", err)
	}

	// Stratis always uses XFS
	fsType := "xfs"

	// Mount the filesystem
	if err := d.mounter.Mount(fs.DevicePath, mountPoint, fsType); err != nil {
		return nil, fmt.Errorf("mount: %w", err)
	}

	log.Info("volume mounted", "name", req.Name, "device", fs.DevicePath, "path", mountPoint, "fs", fsType)
	return &volume.MountResponse{Mountpoint: mountPoint}, nil
}

// Unmount unmounts a volume
func (d *Driver) Unmount(req *volume.UnmountRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	log.Debug("unmounting volume", "name", req.Name, "id", req.ID)

	// Check if filesystem exists
	fs, err := d.stratis.GetByName(req.Name)
	if err != nil {
		if errors.Is(err, stratis.ErrNotFound) {
			return fmt.Errorf("volume %s not found", req.Name)
		}
		return fmt.Errorf("get volume: %w", err)
	}

	mountPoint := d.mountPointPath(req.Name)

	// Check if mounted at expected location
	existingMount, err := d.mounter.GetMountPoint(fs.DevicePath)
	if err != nil {
		return fmt.Errorf("check mount status: %w", err)
	}

	if existingMount == "" {
		// Not mounted
		log.Debug("volume not mounted", "name", req.Name)
		return nil
	}

	if existingMount != mountPoint {
		return fmt.Errorf("volume %s is mounted at %s instead of expected %s", req.Name, existingMount, mountPoint)
	}

	// Unmount
	if err := d.mounter.Unmount(mountPoint); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}

	// Remove mountpoint directory
	if err := os.Remove(mountPoint); err != nil && !os.IsNotExist(err) {
		log.Warn("failed to remove mountpoint directory", "path", mountPoint, "error", err)
	}

	log.Info("volume unmounted", "name", req.Name)
	return nil
}

// Path returns the mount path for a volume
func (d *Driver) Path(req *volume.PathRequest) (*volume.PathResponse, error) {
	log.Debug("getting path", "name", req.Name)

	// Check if filesystem exists
	fs, err := d.stratis.GetByName(req.Name)
	if err != nil {
		if errors.Is(err, stratis.ErrNotFound) {
			return nil, fmt.Errorf("volume %s not found", req.Name)
		}
		return nil, fmt.Errorf("get volume: %w", err)
	}

	mountPoint := d.mountPointPath(req.Name)

	// Check if mounted
	existingMount, err := d.mounter.GetMountPoint(fs.DevicePath)
	if err != nil {
		return nil, fmt.Errorf("check mount status: %w", err)
	}

	if existingMount == "" {
		return nil, fmt.Errorf("volume %s is not mounted", req.Name)
	}

	if existingMount != mountPoint {
		return nil, fmt.Errorf("volume %s is mounted at %s instead of expected %s", req.Name, existingMount, mountPoint)
	}

	return &volume.PathResponse{Mountpoint: mountPoint}, nil
}

// Get returns information about a volume
func (d *Driver) Get(req *volume.GetRequest) (*volume.GetResponse, error) {
	log.Debug("getting volume info", "name", req.Name)

	fs, err := d.stratis.GetByName(req.Name)
	if err != nil {
		if errors.Is(err, stratis.ErrNotFound) {
			return nil, fmt.Errorf("volume %s not found", req.Name)
		}
		return nil, fmt.Errorf("get volume: %w", err)
	}

	mountPoint := d.mountPointPath(req.Name)

	// Check if mounted
	existingMount, err := d.mounter.GetMountPoint(fs.DevicePath)
	if err != nil {
		// Non-fatal, just set mountpoint to empty
		existingMount = ""
	}

	var currentMountPoint string
	if existingMount == mountPoint {
		currentMountPoint = mountPoint
	}

	status := map[string]any{
		"total":  fs.Total,
		"used":   fs.Used,
		"free":   fs.Free,
		"device": fs.DevicePath,
	}
	if fs.SizeLimit != nil {
		status["sizeLimit"] = *fs.SizeLimit
	}

	return &volume.GetResponse{
		Volume: &volume.Volume{
			Name:       req.Name,
			Mountpoint: currentMountPoint,
			Status:     status,
		},
	}, nil
}

// List returns all volumes
func (d *Driver) List() (*volume.ListResponse, error) {
	log.Debug("listing volumes")

	filesystems, err := d.stratis.List()
	if err != nil {
		return nil, fmt.Errorf("list filesystems: %w", err)
	}

	var volumes []*volume.Volume
	for _, fs := range filesystems {
		mountPoint := d.mountPointPath(fs.Name)

		// Check if mounted
		existingMount, _ := d.mounter.GetMountPoint(fs.DevicePath)
		var currentMountPoint string
		if existingMount == mountPoint {
			currentMountPoint = mountPoint
		}

		volumes = append(volumes, &volume.Volume{
			Name:       fs.Name,
			Mountpoint: currentMountPoint,
		})
	}

	return &volume.ListResponse{Volumes: volumes}, nil
}

// Capabilities returns the driver capabilities
func (d *Driver) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{
			Scope: "local",
		},
	}
}

// mountPointPath returns the mount point path for a volume
func (d *Driver) mountPointPath(name string) string {
	return filepath.Join(d.mountPath, name)
}

// prepareMountPoint prepares the mount point directory
func (d *Driver) prepareMountPoint(path string) error {
	// Check if directory exists
	info, err := os.Stat(path)
	if err == nil {
		// Directory exists
		if !info.IsDir() {
			return fmt.Errorf("mount point %s exists but is not a directory", path)
		}

		// Check if empty
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read directory: %w", err)
		}

		if len(entries) > 0 {
			return fmt.Errorf("mount point %s exists and is not empty", path)
		}

		// Empty directory, recreate with proper permissions
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove existing directory: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat mount point: %w", err)
	}

	// Create directory with 0755 permissions
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create mount point: %w", err)
	}

	return nil
}

// parseSize parses a size string with IEC or SI units
func parseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	// Find where the number ends and unit begins
	var numPart string
	var unitPart string
	for i, c := range s {
		if c >= '0' && c <= '9' || c == '.' {
			numPart = s[:i+1]
		} else {
			unitPart = strings.TrimSpace(s[i:])
			break
		}
	}
	if unitPart == "" && numPart == "" {
		numPart = s
	}

	// Parse number
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %w", err)
	}

	if num < 0 {
		return 0, fmt.Errorf("size cannot be negative")
	}

	// Parse unit
	unitPart = strings.ToUpper(unitPart)

	var multiplier float64
	switch unitPart {
	case "", "B":
		multiplier = 1
	case "K", "KB":
		multiplier = 1000
	case "KI", "KIB":
		multiplier = 1024
	case "M", "MB":
		multiplier = 1000 * 1000
	case "MI", "MIB":
		multiplier = 1024 * 1024
	case "G", "GB":
		multiplier = 1000 * 1000 * 1000
	case "GI", "GIB":
		multiplier = 1024 * 1024 * 1024
	case "T", "TB":
		multiplier = 1000 * 1000 * 1000 * 1000
	case "TI", "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit: %s", unitPart)
	}

	return uint64(num * multiplier), nil
}
