package stratis

import (
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/kriansa/podman-volume-stratis/internal/log"
)

const (
	// DBus service and interface constants
	dbusService       = "org.storage.stratis3"
	dbusRootPath      = "/org/storage/stratis3"
	dbusObjectManager = "org.freedesktop.DBus.ObjectManager"

	// Interface versions - using r8 as the latest stable
	dbusPoolInterface       = "org.storage.stratis3.pool.r8"
	dbusFilesystemInterface = "org.storage.stratis3.filesystem.r8"
)

// DBusManager implements Manager using the stratisd DBus API
type DBusManager struct {
	pool      string
	poolPath  dbus.ObjectPath // cached pool object path
	conn      DBusConnection
	connectFn func() (DBusConnection, error) // for reconnection
}

// DBusManagerOption is a functional option for DBusManager
type DBusManagerOption func(*DBusManager)

// WithConnection sets a custom DBus connection (for testing)
func WithConnection(conn DBusConnection) DBusManagerOption {
	return func(m *DBusManager) {
		m.conn = conn
		m.connectFn = nil // disable reconnection when using custom connection
	}
}

// NewDBusManager creates a new Stratis DBus manager for the given pool
func NewDBusManager(pool string, opts ...DBusManagerOption) (*DBusManager, error) {
	m := &DBusManager{
		pool:      pool,
		connectFn: ConnectSystemBus,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Connect if no custom connection provided
	if m.conn == nil {
		conn, err := m.connectFn()
		if err != nil {
			return nil, fmt.Errorf("connect to system bus: %w", err)
		}
		m.conn = conn
	}

	return m, nil
}

// Close closes the DBus connection
func (m *DBusManager) Close() error {
	if m.conn != nil {
		return m.conn.Close()
	}
	return nil
}

// getManagedObjects calls GetManagedObjects on the ObjectManager interface
// Returns: map[ObjectPath]map[InterfaceName]map[PropertyName]Variant
func (m *DBusManager) getManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	obj := m.conn.Object(dbusService, dbus.ObjectPath(dbusRootPath))

	var result map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	call := obj.Call(dbusObjectManager+".GetManagedObjects", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("GetManagedObjects: %w", call.Err)
	}

	if err := call.Store(&result); err != nil {
		return nil, fmt.Errorf("store GetManagedObjects result: %w", err)
	}

	return result, nil
}

// findPoolPath finds the object path for our configured pool
func (m *DBusManager) findPoolPath() (dbus.ObjectPath, error) {
	// Return cached path if available
	if m.poolPath != "" {
		return m.poolPath, nil
	}

	objects, err := m.getManagedObjects()
	if err != nil {
		return "", err
	}

	for path, interfaces := range objects {
		poolProps, ok := interfaces[dbusPoolInterface]
		if !ok {
			continue
		}

		nameVariant, ok := poolProps["Name"]
		if !ok {
			continue
		}

		name, ok := nameVariant.Value().(string)
		if ok && name == m.pool {
			m.poolPath = path
			return path, nil
		}
	}

	return "", fmt.Errorf("pool %q not found", m.pool)
}

// findFilesystemPath finds the DBus object path for a filesystem by name
func (m *DBusManager) findFilesystemPath(name string) (dbus.ObjectPath, error) {
	poolPath, err := m.findPoolPath()
	if err != nil {
		return "", err
	}

	objects, err := m.getManagedObjects()
	if err != nil {
		return "", err
	}

	for path, interfaces := range objects {
		fsProps, ok := interfaces[dbusFilesystemInterface]
		if !ok {
			continue
		}

		// Check pool
		poolVariant, ok := fsProps["Pool"]
		if !ok {
			continue
		}
		fsPoolPath, ok := poolVariant.Value().(dbus.ObjectPath)
		if !ok || fsPoolPath != poolPath {
			continue
		}

		// Check name
		nameVariant, ok := fsProps["Name"]
		if !ok {
			continue
		}
		fsName, ok := nameVariant.Value().(string)
		if ok && fsName == name {
			return path, nil
		}
	}

	return "", ErrNotFound
}

// parseFilesystemFromProps creates a Filesystem from DBus property map
func (m *DBusManager) parseFilesystemFromProps(props map[string]dbus.Variant) (*Filesystem, error) {
	fs := &Filesystem{
		Pool: m.pool,
	}

	// Name (required)
	if v, ok := props["Name"]; ok {
		if name, ok := v.Value().(string); ok {
			fs.Name = name
		}
	}
	if fs.Name == "" {
		return nil, fmt.Errorf("missing Name property")
	}

	// UUID (required)
	if v, ok := props["Uuid"]; ok {
		if uuid, ok := v.Value().(string); ok {
			fs.UUID = uuid
		}
	}

	// Devnode (required)
	if v, ok := props["Devnode"]; ok {
		if devnode, ok := v.Value().(string); ok {
			fs.DevicePath = devnode
		}
	}
	if fs.DevicePath == "" {
		return nil, fmt.Errorf("missing Devnode property")
	}

	// Size - stored as string representation of bytes
	if v, ok := props["Size"]; ok {
		if sizeStr, ok := v.Value().(string); ok {
			var size uint64
			if _, err := fmt.Sscanf(sizeStr, "%d", &size); err == nil {
				fs.Total = size
			}
		}
	}

	// Used - optional property (bool, string) tuple
	if v, ok := props["Used"]; ok {
		if used := extractOptionalString(v); used != nil {
			var usedBytes uint64
			if _, err := fmt.Sscanf(*used, "%d", &usedBytes); err == nil {
				fs.Used = usedBytes
			}
		}
	}

	// Calculate Free
	if fs.Total > fs.Used {
		fs.Free = fs.Total - fs.Used
	}

	// SizeLimit - optional property (bool, string) tuple
	if v, ok := props["SizeLimit"]; ok {
		if limit := extractOptionalString(v); limit != nil {
			var limitBytes uint64
			if _, err := fmt.Sscanf(*limit, "%d", &limitBytes); err == nil {
				fs.SizeLimit = &limitBytes
			}
		}
	}

	return fs, nil
}

// extractOptionalString extracts an optional string from a (bool, string) tuple variant
func extractOptionalString(v dbus.Variant) *string {
	// stratisd represents optional values as (bool, value) tuples
	tuple, ok := v.Value().([]any)
	if !ok || len(tuple) != 2 {
		return nil
	}

	hasValue, ok := tuple[0].(bool)
	if !ok || !hasValue {
		return nil
	}

	val, ok := tuple[1].(string)
	if !ok {
		return nil
	}

	return &val
}

// checkReturnCode checks the stratisd return code tuple (return_code, message)
func checkReturnCode(returnCode uint16, message string) error {
	if returnCode == 0 {
		return nil
	}
	return fmt.Errorf("stratisd error (code %d): %s", returnCode, message)
}

// PoolExists checks if the configured pool exists
func (m *DBusManager) PoolExists() (bool, error) {
	log.Debug("checking pool exists via dbus", "pool", m.pool)

	_, err := m.findPoolPath()
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("check pool: %w", err)
	}

	return true, nil
}

// List returns all filesystems in the pool
func (m *DBusManager) List() ([]Filesystem, error) {
	log.Debug("listing filesystems via dbus", "pool", m.pool)

	poolPath, err := m.findPoolPath()
	if err != nil {
		return nil, fmt.Errorf("find pool: %w", err)
	}

	objects, err := m.getManagedObjects()
	if err != nil {
		return nil, fmt.Errorf("get managed objects: %w", err)
	}

	var filesystems []Filesystem

	for _, interfaces := range objects {
		fsProps, ok := interfaces[dbusFilesystemInterface]
		if !ok {
			continue
		}

		// Check if this filesystem belongs to our pool
		poolVariant, ok := fsProps["Pool"]
		if !ok {
			continue
		}

		fsPoolPath, ok := poolVariant.Value().(dbus.ObjectPath)
		if !ok || fsPoolPath != poolPath {
			continue
		}

		fs, err := m.parseFilesystemFromProps(fsProps)
		if err != nil {
			log.Debug("failed to parse filesystem", "error", err)
			continue
		}

		filesystems = append(filesystems, *fs)
	}

	return filesystems, nil
}

// GetByName returns the filesystem with the given name
func (m *DBusManager) GetByName(name string) (*Filesystem, error) {
	log.Debug("getting filesystem by name via dbus", "name", name, "pool", m.pool)

	poolPath, err := m.findPoolPath()
	if err != nil {
		return nil, fmt.Errorf("find pool: %w", err)
	}

	objects, err := m.getManagedObjects()
	if err != nil {
		return nil, fmt.Errorf("get managed objects: %w", err)
	}

	for _, interfaces := range objects {
		fsProps, ok := interfaces[dbusFilesystemInterface]
		if !ok {
			continue
		}

		// Check pool
		poolVariant, ok := fsProps["Pool"]
		if !ok {
			continue
		}
		fsPoolPath, ok := poolVariant.Value().(dbus.ObjectPath)
		if !ok || fsPoolPath != poolPath {
			continue
		}

		// Check name
		nameVariant, ok := fsProps["Name"]
		if !ok {
			continue
		}
		fsName, ok := nameVariant.Value().(string)
		if !ok || fsName != name {
			continue
		}

		return m.parseFilesystemFromProps(fsProps)
	}

	return nil, ErrNotFound
}

// Create creates a new filesystem with the given name and optional size limit
func (m *DBusManager) Create(name string, sizeLimit *uint64) (*Filesystem, error) {
	log.Debug("creating filesystem via dbus", "name", name, "pool", m.pool, "sizeLimit", sizeLimit)

	poolPath, err := m.findPoolPath()
	if err != nil {
		return nil, fmt.Errorf("find pool: %w", err)
	}

	poolObj := m.conn.Object(dbusService, poolPath)

	// Build filesystem specs for CreateFilesystems
	// The DBus signature is: a(s(bs)(bs))
	// Each spec is a struct: (name: string, size: (bool, string), limit: (bool, string))
	hasLimit := sizeLimit != nil
	var limitStr string
	if hasLimit {
		limitStr = fmt.Sprintf("%d", *sizeLimit)
	}

	// Use proper struct types for DBus serialization
	// Each inner (bool, string) tuple is a struct in DBus
	type optionalString struct {
		HasValue bool
		Value    string
	}
	type filesystemSpec struct {
		Name      string
		Size      optionalString
		SizeLimit optionalString
	}

	// When setting a size limit, we also need to set the initial size to the same value
	// Otherwise stratisd uses a default size that may exceed the limit
	specs := []filesystemSpec{
		{
			Name:      name,
			Size:      optionalString{HasValue: hasLimit, Value: limitStr},
			SizeLimit: optionalString{HasValue: hasLimit, Value: limitStr},
		},
	}

	// Call CreateFilesystems
	// Returns: ((changed: bool, results: [(path, name)]), return_code, message)
	call := poolObj.Call(dbusPoolInterface+".CreateFilesystems", 0, specs)
	if call.Err != nil {
		return nil, fmt.Errorf("CreateFilesystems: %w", call.Err)
	}

	// Parse the response - it's a complex nested structure
	// Response: ((changed: bool, results: [(path, name)]), return_code, message)
	if len(call.Body) < 3 {
		return nil, fmt.Errorf("unexpected response format from CreateFilesystems")
	}

	returnCode, ok := call.Body[1].(uint16)
	if !ok {
		return nil, fmt.Errorf("unexpected return code type: got %T", call.Body[1])
	}

	message, ok := call.Body[2].(string)
	if !ok {
		message = ""
	}

	if err := checkReturnCode(returnCode, message); err != nil {
		return nil, fmt.Errorf("create filesystem: %w", err)
	}

	// Invalidate pool path cache to refresh on next query
	m.poolPath = ""

	// Get the created filesystem with retry - DBus may take a moment to reflect the new filesystem
	var fs *Filesystem
	for i := range 10 {
		fs, err = m.GetByName(name)
		if err == nil {
			break
		}
		if i < 9 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("get created filesystem: %w", err)
	}

	log.Debug("filesystem created via dbus", "name", name, "device", fs.DevicePath)
	return fs, nil
}

// Delete removes the filesystem with the given name
func (m *DBusManager) Delete(name string) error {
	log.Debug("deleting filesystem via dbus", "name", name, "pool", m.pool)

	poolPath, err := m.findPoolPath()
	if err != nil {
		return fmt.Errorf("find pool: %w", err)
	}

	// First, find the filesystem path
	fsPath, err := m.findFilesystemPath(name)
	if err != nil {
		return fmt.Errorf("find filesystem: %w", err)
	}

	poolObj := m.conn.Object(dbusService, poolPath)

	// Call DestroyFilesystems with array of paths
	fsPaths := []dbus.ObjectPath{fsPath}

	// Returns: ((changed: bool, uuids: [string]), return_code, message)
	call := poolObj.Call(dbusPoolInterface+".DestroyFilesystems", 0, fsPaths)
	if call.Err != nil {
		return fmt.Errorf("DestroyFilesystems: %w", call.Err)
	}

	// Parse the response
	if len(call.Body) < 3 {
		return fmt.Errorf("unexpected response format from DestroyFilesystems")
	}

	returnCode, ok := call.Body[1].(uint16)
	if !ok {
		return fmt.Errorf("unexpected return code type")
	}

	message, ok := call.Body[2].(string)
	if !ok {
		message = ""
	}

	if err := checkReturnCode(returnCode, message); err != nil {
		return fmt.Errorf("delete filesystem: %w", err)
	}

	// Invalidate pool path cache
	m.poolPath = ""

	log.Debug("filesystem deleted via dbus", "name", name)
	return nil
}
