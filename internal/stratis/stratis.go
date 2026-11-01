package stratis

import (
	"fmt"
	"sync"
)

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
	// Returns ErrNotFound if not found
	GetByName(name string) (*Filesystem, error)
}

// ErrNotFound is returned when a filesystem is not found
var ErrNotFound = fmt.Errorf("filesystem not found")

// NewManager creates a Manager based on the specified backend. Its Create and Delete run one at a
// time, whoever calls them; see OneChangeAtATime.
func NewManager(pool, backend string) (Manager, error) {
	var m Manager
	switch backend {
	case "cli":
		m = NewCLIManager(pool)
	case "dbus":
		dbusManager, err := NewDBusManager(pool)
		if err != nil {
			return nil, err
		}
		m = dbusManager
	default:
		return nil, fmt.Errorf("unknown backend: %s (use 'dbus' or 'cli')", backend)
	}
	// Whatever the backend: they all end up asking stratisd
	return OneChangeAtATime(m), nil
}

// OneChangeAtATime returns m with its Create and Delete running one at a time, while lookups still
// run side by side. stratisd 3.9 stops answering altogether, until it is restarted, when it is
// asked to create several filesystems at once. Both backends end up asking stratisd, so the rule
// lives here rather than with any one caller.
func OneChangeAtATime(m Manager) Manager {
	return &oneChangeAtATime{Manager: m}
}

type oneChangeAtATime struct {
	Manager
	mu sync.Mutex
}

func (m *oneChangeAtATime) Create(name string, sizeLimit *uint64) (*Filesystem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Manager.Create(name, sizeLimit)
}

func (m *oneChangeAtATime) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Manager.Delete(name)
}
