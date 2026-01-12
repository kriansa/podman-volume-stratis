package stratis

import (
	"context"
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/kriansa/podman-volume-stratis/internal/log"
)

func TestMain(m *testing.M) {
	// Initialize logger for tests
	log.Setup(false)
	os.Exit(m.Run())
}

// mockBusObject implements dbus.BusObject for testing
type mockBusObject struct {
	callResults map[string]*dbus.Call
}

func (m *mockBusObject) Call(method string, flags dbus.Flags, args ...any) *dbus.Call {
	if call, ok := m.callResults[method]; ok {
		return call
	}
	return &dbus.Call{Err: dbus.ErrMsgNoObject}
}

func (m *mockBusObject) CallWithContext(_ context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
	return m.Call(method, flags, args...)
}

func (m *mockBusObject) Go(method string, flags dbus.Flags, ch chan *dbus.Call, args ...any) *dbus.Call {
	return m.Call(method, flags, args...)
}

func (m *mockBusObject) GoWithContext(_ context.Context, method string, flags dbus.Flags, ch chan *dbus.Call, args ...any) *dbus.Call {
	return m.Call(method, flags, args...)
}

func (m *mockBusObject) AddMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return &dbus.Call{}
}

func (m *mockBusObject) RemoveMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return &dbus.Call{}
}

func (m *mockBusObject) GetProperty(p string) (dbus.Variant, error) {
	return dbus.Variant{}, nil
}

func (m *mockBusObject) StoreProperty(p string, value any) error {
	return nil
}

func (m *mockBusObject) SetProperty(p string, v any) error {
	return nil
}

func (m *mockBusObject) Destination() string {
	return dbusService
}

func (m *mockBusObject) Path() dbus.ObjectPath {
	return dbus.ObjectPath(dbusRootPath)
}

// mockDBusConnection implements DBusConnection for testing
type mockDBusConnection struct {
	objects map[dbus.ObjectPath]*mockBusObject
}

func (m *mockDBusConnection) Object(dest string, path dbus.ObjectPath) dbus.BusObject {
	if obj, ok := m.objects[path]; ok {
		return obj
	}
	// Return a default mock object with empty results
	return &mockBusObject{callResults: map[string]*dbus.Call{}}
}

func (m *mockDBusConnection) Close() error {
	return nil
}

// Helper to create mock managed objects
func makeManagedObjects(pools []mockPool, filesystems []mockFilesystem) map[dbus.ObjectPath]map[string]map[string]dbus.Variant {
	result := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)

	for _, p := range pools {
		result[p.path] = map[string]map[string]dbus.Variant{
			dbusPoolInterface: {
				"Name": dbus.MakeVariant(p.name),
			},
		}
	}

	for _, fs := range filesystems {
		result[fs.path] = map[string]map[string]dbus.Variant{
			dbusFilesystemInterface: {
				"Name":      dbus.MakeVariant(fs.name),
				"Pool":      dbus.MakeVariant(fs.poolPath),
				"Uuid":      dbus.MakeVariant(fs.uuid),
				"Devnode":   dbus.MakeVariant(fs.devnode),
				"Size":      dbus.MakeVariant(fs.size),
				"Used":      dbus.MakeVariant([]any{fs.usedPresent, fs.used}),
				"SizeLimit": dbus.MakeVariant([]any{fs.limitPresent, fs.limit}),
			},
		}
	}

	return result
}

type mockPool struct {
	path dbus.ObjectPath
	name string
}

type mockFilesystem struct {
	path         dbus.ObjectPath
	name         string
	poolPath     dbus.ObjectPath
	uuid         string
	devnode      string
	size         string
	used         string
	usedPresent  bool
	limit        string
	limitPresent bool
}

func TestDBusManager_PoolExists(t *testing.T) {
	poolPath := dbus.ObjectPath("/org/storage/stratis3/pool/1")

	tests := []struct {
		name    string
		pool    string
		pools   []mockPool
		want    bool
		wantErr bool
	}{
		{
			name: "pool exists",
			pool: "test-pool",
			pools: []mockPool{
				{path: poolPath, name: "test-pool"},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:    "pool not found",
			pool:    "nonexistent",
			pools:   []mockPool{},
			want:    false,
			wantErr: false,
		},
		{
			name: "different pool exists",
			pool: "my-pool",
			pools: []mockPool{
				{path: poolPath, name: "other-pool"},
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managedObjects := makeManagedObjects(tt.pools, nil)

			rootObj := &mockBusObject{
				callResults: map[string]*dbus.Call{
					dbusObjectManager + ".GetManagedObjects": {
						Body: []any{managedObjects},
					},
				},
			}

			conn := &mockDBusConnection{
				objects: map[dbus.ObjectPath]*mockBusObject{
					dbus.ObjectPath(dbusRootPath): rootObj,
				},
			}

			m, err := NewDBusManager(tt.pool, WithConnection(conn))
			if err != nil {
				t.Fatalf("NewDBusManager() error = %v", err)
			}

			got, err := m.PoolExists()
			if (err != nil) != tt.wantErr {
				t.Errorf("PoolExists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("PoolExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDBusManager_List(t *testing.T) {
	poolPath := dbus.ObjectPath("/org/storage/stratis3/pool/1")
	fsPath1 := dbus.ObjectPath("/org/storage/stratis3/filesystem/1")
	fsPath2 := dbus.ObjectPath("/org/storage/stratis3/filesystem/2")

	tests := []struct {
		name        string
		pool        string
		pools       []mockPool
		filesystems []mockFilesystem
		wantCount   int
		wantNames   []string
		wantErr     bool
	}{
		{
			name: "list filesystems",
			pool: "test-pool",
			pools: []mockPool{
				{path: poolPath, name: "test-pool"},
			},
			filesystems: []mockFilesystem{
				{
					path:        fsPath1,
					name:        "vol1",
					poolPath:    poolPath,
					uuid:        "uuid-1",
					devnode:     "/dev/stratis/test-pool/vol1",
					size:        "1073741824",
					used:        "77594624",
					usedPresent: true,
				},
				{
					path:        fsPath2,
					name:        "vol2",
					poolPath:    poolPath,
					uuid:        "uuid-2",
					devnode:     "/dev/stratis/test-pool/vol2",
					size:        "2147483648",
					used:        "0",
					usedPresent: true,
				},
			},
			wantCount: 2,
			wantNames: []string{"vol1", "vol2"},
			wantErr:   false,
		},
		{
			name: "empty pool",
			pool: "test-pool",
			pools: []mockPool{
				{path: poolPath, name: "test-pool"},
			},
			filesystems: []mockFilesystem{},
			wantCount:   0,
			wantNames:   []string{},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managedObjects := makeManagedObjects(tt.pools, tt.filesystems)

			rootObj := &mockBusObject{
				callResults: map[string]*dbus.Call{
					dbusObjectManager + ".GetManagedObjects": {
						Body: []any{managedObjects},
					},
				},
			}

			conn := &mockDBusConnection{
				objects: map[dbus.ObjectPath]*mockBusObject{
					dbus.ObjectPath(dbusRootPath): rootObj,
				},
			}

			m, err := NewDBusManager(tt.pool, WithConnection(conn))
			if err != nil {
				t.Fatalf("NewDBusManager() error = %v", err)
			}

			got, err := m.List()
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != tt.wantCount {
				t.Errorf("List() returned %d filesystems, want %d", len(got), tt.wantCount)
			}

			// Check names
			gotNames := make(map[string]bool)
			for _, fs := range got {
				gotNames[fs.Name] = true
			}
			for _, name := range tt.wantNames {
				if !gotNames[name] {
					t.Errorf("List() missing filesystem %q", name)
				}
			}
		})
	}
}

func TestDBusManager_GetByName(t *testing.T) {
	poolPath := dbus.ObjectPath("/org/storage/stratis3/pool/1")
	fsPath := dbus.ObjectPath("/org/storage/stratis3/filesystem/1")

	tests := []struct {
		name        string
		pool        string
		fsName      string
		pools       []mockPool
		filesystems []mockFilesystem
		wantErr     error
	}{
		{
			name:   "filesystem found",
			pool:   "test-pool",
			fsName: "vol1",
			pools: []mockPool{
				{path: poolPath, name: "test-pool"},
			},
			filesystems: []mockFilesystem{
				{
					path:        fsPath,
					name:        "vol1",
					poolPath:    poolPath,
					uuid:        "uuid-1",
					devnode:     "/dev/stratis/test-pool/vol1",
					size:        "1073741824",
					used:        "77594624",
					usedPresent: true,
				},
			},
			wantErr: nil,
		},
		{
			name:   "filesystem not found",
			pool:   "test-pool",
			fsName: "nonexistent",
			pools: []mockPool{
				{path: poolPath, name: "test-pool"},
			},
			filesystems: []mockFilesystem{
				{
					path:        fsPath,
					name:        "vol1",
					poolPath:    poolPath,
					uuid:        "uuid-1",
					devnode:     "/dev/stratis/test-pool/vol1",
					size:        "1073741824",
					used:        "77594624",
					usedPresent: true,
				},
			},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			managedObjects := makeManagedObjects(tt.pools, tt.filesystems)

			rootObj := &mockBusObject{
				callResults: map[string]*dbus.Call{
					dbusObjectManager + ".GetManagedObjects": {
						Body: []any{managedObjects},
					},
				},
			}

			conn := &mockDBusConnection{
				objects: map[dbus.ObjectPath]*mockBusObject{
					dbus.ObjectPath(dbusRootPath): rootObj,
				},
			}

			m, err := NewDBusManager(tt.pool, WithConnection(conn))
			if err != nil {
				t.Fatalf("NewDBusManager() error = %v", err)
			}

			got, err := m.GetByName(tt.fsName)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("GetByName() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("GetByName() unexpected error = %v", err)
				return
			}

			if got.Name != tt.fsName {
				t.Errorf("GetByName() returned filesystem with name %q, want %q", got.Name, tt.fsName)
			}
		})
	}
}

func TestParseFilesystemFromProps(t *testing.T) {
	m := &DBusManager{pool: "test-pool"}

	tests := []struct {
		name      string
		props     map[string]dbus.Variant
		wantName  string
		wantUUID  string
		wantDev   string
		wantTotal uint64
		wantUsed  uint64
		wantFree  uint64
		wantLimit *uint64
		wantErr   bool
	}{
		{
			name: "full filesystem",
			props: map[string]dbus.Variant{
				"Name":      dbus.MakeVariant("vol1"),
				"Uuid":      dbus.MakeVariant("uuid-1"),
				"Devnode":   dbus.MakeVariant("/dev/stratis/test-pool/vol1"),
				"Size":      dbus.MakeVariant("1073741824"),
				"Used":      dbus.MakeVariant([]any{true, "77594624"}),
				"SizeLimit": dbus.MakeVariant([]any{false, ""}),
			},
			wantName:  "vol1",
			wantUUID:  "uuid-1",
			wantDev:   "/dev/stratis/test-pool/vol1",
			wantTotal: 1073741824,
			wantUsed:  77594624,
			wantFree:  1073741824 - 77594624,
			wantLimit: nil,
			wantErr:   false,
		},
		{
			name: "filesystem with size limit",
			props: map[string]dbus.Variant{
				"Name":      dbus.MakeVariant("vol2"),
				"Uuid":      dbus.MakeVariant("uuid-2"),
				"Devnode":   dbus.MakeVariant("/dev/stratis/test-pool/vol2"),
				"Size":      dbus.MakeVariant("2147483648"),
				"Used":      dbus.MakeVariant([]any{true, "100000000"}),
				"SizeLimit": dbus.MakeVariant([]any{true, "5368709120"}),
			},
			wantName:  "vol2",
			wantUUID:  "uuid-2",
			wantDev:   "/dev/stratis/test-pool/vol2",
			wantTotal: 2147483648,
			wantUsed:  100000000,
			wantFree:  2147483648 - 100000000,
			wantLimit: ptrUint64(5368709120),
			wantErr:   false,
		},
		{
			name: "missing name",
			props: map[string]dbus.Variant{
				"Uuid":    dbus.MakeVariant("uuid-1"),
				"Devnode": dbus.MakeVariant("/dev/stratis/test-pool/vol1"),
			},
			wantErr: true,
		},
		{
			name: "missing devnode",
			props: map[string]dbus.Variant{
				"Name": dbus.MakeVariant("vol1"),
				"Uuid": dbus.MakeVariant("uuid-1"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := m.parseFilesystemFromProps(tt.props)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFilesystemFromProps() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.UUID != tt.wantUUID {
				t.Errorf("UUID = %q, want %q", got.UUID, tt.wantUUID)
			}
			if got.DevicePath != tt.wantDev {
				t.Errorf("DevicePath = %q, want %q", got.DevicePath, tt.wantDev)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
			if got.Used != tt.wantUsed {
				t.Errorf("Used = %d, want %d", got.Used, tt.wantUsed)
			}
			if got.Free != tt.wantFree {
				t.Errorf("Free = %d, want %d", got.Free, tt.wantFree)
			}
			if tt.wantLimit == nil {
				if got.SizeLimit != nil {
					t.Errorf("SizeLimit = %d, want nil", *got.SizeLimit)
				}
			} else {
				if got.SizeLimit == nil {
					t.Errorf("SizeLimit = nil, want %d", *tt.wantLimit)
				} else if *got.SizeLimit != *tt.wantLimit {
					t.Errorf("SizeLimit = %d, want %d", *got.SizeLimit, *tt.wantLimit)
				}
			}
		})
	}
}

func ptrUint64(v uint64) *uint64 {
	return &v
}

func TestCheckReturnCode(t *testing.T) {
	tests := []struct {
		name       string
		returnCode uint16
		message    string
		wantErr    bool
	}{
		{
			name:       "success",
			returnCode: 0,
			message:    "",
			wantErr:    false,
		},
		{
			name:       "error",
			returnCode: 1,
			message:    "something went wrong",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkReturnCode(tt.returnCode, tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkReturnCode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
