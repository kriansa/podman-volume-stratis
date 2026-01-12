package stratis

import "fmt"

// Filesystem represents a Stratis filesystem
type Filesystem struct {
	// Name is the filesystem name (same as volume name)
	Name string
	// Pool is the Stratis pool name
	Pool string
	// DevicePath is the device path (e.g., /dev/stratis/pool/name)
	DevicePath string
	// Total is the total allocated size in bytes
	Total uint64
	// Used is the used space in bytes
	Used uint64
	// Free is the free space in bytes
	Free uint64
	// SizeLimit is the optional size limit in bytes (nil = thin provisioned)
	SizeLimit *uint64
	// UUID is the filesystem UUID
	UUID string
}

// Manager defines the interface for Stratis filesystem management operations
type Manager interface {
	// PoolExists checks if the configured pool exists
	PoolExists() (bool, error)

	// List returns all filesystems in the pool
	List() ([]Filesystem, error)

	// Create creates a new filesystem with the given name and optional size limit
	// If sizeLimit is nil, the filesystem is thin-provisioned without a limit
	// Returns the created filesystem
	Create(name string, sizeLimit *uint64) (*Filesystem, error)

	// Delete removes the filesystem with the given name
	Delete(name string) error

	// GetByName returns the filesystem with the given name
	// Returns nil if not found
	GetByName(name string) (*Filesystem, error)
}

// ErrNotFound is returned when a filesystem is not found
var ErrNotFound = fmt.Errorf("filesystem not found")

// NewManager creates a Manager based on the specified backend
func NewManager(pool, backend string) (Manager, error) {
	switch backend {
	case "cli":
		return NewCLIManager(pool), nil
	case "dbus":
		return NewDBusManager(pool)
	default:
		return nil, fmt.Errorf("unknown backend: %s (use 'dbus' or 'cli')", backend)
	}
}
