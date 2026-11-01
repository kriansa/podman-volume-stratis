package driver

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/kriansa/podman-volume-stratis/internal/log"
	"github.com/kriansa/podman-volume-stratis/internal/stratis"
)

func TestMain(m *testing.M) {
	log.Setup(false)
	os.Exit(m.Run())
}

// How long a test waits for something that should happen. Only reached when a test fails.
const patience = 5 * time.Second

// fakeBackend stands in for both stratisd and the kernel's mount table, keeping everything in
// memory. Some of its operations can be held: every call to one announces its volume on entered and
// then waits for release to close, so a test can keep an operation in progress for as long as it
// needs.
type fakeBackend struct {
	held    map[string]bool // any of "create", "delete", "mount" and "unmount"
	entered chan string
	release chan struct{}

	mu      sync.Mutex
	volumes map[string]bool
	mounts  map[string]string // device -> mount point
	lookups map[string]int    // how many times each volume was looked up
	mounted map[string]int    // how many times each volume was mounted
}

func newFakeBackend(held ...string) *fakeBackend {
	b := &fakeBackend{
		held:    make(map[string]bool),
		entered: make(chan string, 64),
		release: make(chan struct{}),
		volumes: make(map[string]bool),
		mounts:  make(map[string]string),
		lookups: make(map[string]int),
		mounted: make(map[string]int),
	}
	for _, operation := range held {
		b.held[operation] = true
	}
	return b
}

func (b *fakeBackend) hold(operation, name string) {
	if b.held[operation] {
		b.entered <- name
		<-b.release
	}
}

func device(name string) string { return "/dev/stratis/pool/" + name }

// stratis.Manager

func (b *fakeBackend) PoolExists() (bool, error) { return true, nil }

func (b *fakeBackend) List() ([]stratis.Filesystem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var filesystems []stratis.Filesystem
	for name := range b.volumes {
		filesystems = append(filesystems, stratis.Filesystem{Name: name, DevicePath: device(name)})
	}
	return filesystems, nil
}

func (b *fakeBackend) GetByName(name string) (*stratis.Filesystem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lookups[name]++
	if !b.volumes[name] {
		return nil, stratis.ErrNotFound
	}
	return &stratis.Filesystem{Name: name, DevicePath: device(name)}, nil
}

func (b *fakeBackend) Create(name string, _ *uint64) (*stratis.Filesystem, error) {
	b.hold("create", name)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.volumes[name] {
		return nil, fmt.Errorf("filesystem %s already exists", name)
	}
	b.volumes[name] = true
	return &stratis.Filesystem{Name: name, DevicePath: device(name)}, nil
}

func (b *fakeBackend) Delete(name string) error {
	b.hold("delete", name)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.volumes[name] {
		return stratis.ErrNotFound
	}
	if _, ok := b.mounts[device(name)]; ok {
		return fmt.Errorf("filesystem %s is still mounted", name)
	}
	delete(b.volumes, name)
	return nil
}

// mount.Mounter

func (b *fakeBackend) Mount(source, target, _ string) error {
	b.hold("mount", path.Base(source))
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.mounts[source]; ok {
		return fmt.Errorf("%s is already mounted", source)
	}
	b.mounts[source] = target
	b.mounted[path.Base(source)]++
	return nil
}

func (b *fakeBackend) Unmount(target string) error {
	b.hold("unmount", path.Base(target))
	b.mu.Lock()
	defer b.mu.Unlock()
	for source, mountPoint := range b.mounts {
		if mountPoint == target {
			delete(b.mounts, source)
			return nil
		}
	}
	return fmt.Errorf("%s is not mounted", target)
}

func (b *fakeBackend) IsMounted(target string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, mountPoint := range b.mounts {
		if mountPoint == target {
			return true, nil
		}
	}
	return false, nil
}

func (b *fakeBackend) GetMountPoint(source string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mounts[source], nil
}

// Test helpers

func (b *fakeBackend) lookupsOf(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lookups[name]
}

func (b *fakeBackend) isMounted(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.mounts[device(name)]
	return ok
}

func (b *fakeBackend) timesMounted(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mounted[name]
}

func (b *fakeBackend) exists(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.volumes[name]
}

// waitUntilEntered fails the test unless every given volume reaches the held operation.
func (b *fakeBackend) waitUntilEntered(t *testing.T, names ...string) {
	t.Helper()
	timeout := time.After(patience)
	for got := 0; got < len(names); got++ {
		select {
		case <-b.entered:
		case <-timeout:
			close(b.release)
			t.Fatalf("only %d of %d operations got as far as the backend; the rest were waiting", got, len(names))
		}
	}
}

// expectNotEntered fails the test if any operation reaches a held backend call in the next moment.
func (b *fakeBackend) expectNotEntered(t *testing.T, why string) {
	t.Helper()
	select {
	case name := <-b.entered:
		close(b.release)
		t.Fatalf("%s reached the backend: %s", name, why)
	case <-time.After(200 * time.Millisecond):
	}
}

// lockUsers returns how many requests hold or wait for the named volume's lock.
func lockUsers(d *Driver, name string) int {
	d.locks.mu.Lock()
	defer d.locks.mu.Unlock()
	if vl := d.locks.locks[name]; vl != nil {
		return vl.users
	}
	return 0
}

// lockedVolumes returns how many volumes have a lock that is held or waited for.
func lockedVolumes(d *Driver) int {
	d.locks.mu.Lock()
	defer d.locks.mu.Unlock()
	return len(d.locks.locks)
}

// The four operations that take a volume's lock, each with the state a volume has to be in for
// the operation to reach the backend call it is named after.
type operation struct {
	name    string
	held    string // the backend call it reaches
	prepare func(b *fakeBackend, volume string)
	run     func(d *Driver, volume string) error
}

func unmounted(b *fakeBackend, name string) { b.volumes[name] = true }

func mountedAt(mountPath string) func(b *fakeBackend, name string) {
	return func(b *fakeBackend, name string) {
		b.volumes[name] = true
		b.mounts[device(name)] = mountPath + "/" + name
	}
}

func operations(mountPath string) map[string]operation {
	return map[string]operation{
		"create": {
			name:    "create",
			held:    "create",
			prepare: func(*fakeBackend, string) {},
			run: func(d *Driver, name string) error {
				return d.Create(&volume.CreateRequest{Name: name})
			},
		},
		"remove": {
			name:    "remove",
			held:    "delete",
			prepare: unmounted,
			run: func(d *Driver, name string) error {
				return d.Remove(&volume.RemoveRequest{Name: name})
			},
		},
		"mount": {
			name:    "mount",
			held:    "mount",
			prepare: unmounted,
			run: func(d *Driver, name string) error {
				_, err := d.Mount(&volume.MountRequest{Name: name, ID: "container"})
				return err
			},
		},
		"unmount": {
			name:    "unmount",
			held:    "unmount",
			prepare: mountedAt(mountPath),
			run: func(d *Driver, name string) error {
				return d.Unmount(&volume.UnmountRequest{Name: name, ID: "container"})
			},
		},
	}
}

func runInBackground(d *Driver, op operation, name string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- op.run(d, name) }()
	return done
}

func waitFor(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(patience):
		t.Fatal("operation did not finish")
		return nil
	}
}

// Many containers starting or stopping at once, such as at boot, ask for their volumes at the same
// moment. Podman gives up on a request after a few seconds, so mounting or unmounting one volume
// must never wait for other volumes.
func TestMountsOfDifferentVolumesDoNotWaitForEachOther(t *testing.T) {
	for _, name := range []string{"mount", "unmount"} {
		t.Run(name, func(t *testing.T) {
			mountPath := t.TempDir()
			op := operations(mountPath)[name]
			backend := newFakeBackend(op.held)
			d := NewDriver(mountPath, backend, backend)

			volumes := []string{"vol0", "vol1", "vol2", "vol3", "vol4"}
			for _, v := range volumes {
				op.prepare(backend, v)
			}
			var results []<-chan error
			for _, v := range volumes {
				results = append(results, runInBackground(d, op, v))
			}

			// Every operation has to reach the backend while all the others are still inside it.
			backend.waitUntilEntered(t, volumes...)
			close(backend.release)

			for _, done := range results {
				if err := waitFor(t, done); err != nil {
					t.Errorf("%s: %v", name, err)
				}
			}
			if n := lockedVolumes(d); n != 0 {
				t.Errorf("%d volume locks left behind after every request finished", n)
			}
		})
	}
}

// stratisd stops answering when it is asked to create several filesystems at once, so creating and
// deleting go to it one at a time, even for different volumes.
func TestPoolChangesGoToStratisOneAtATime(t *testing.T) {
	for _, pair := range [][2]string{
		{"create", "create"},
		{"create", "remove"},
		{"remove", "create"},
		{"remove", "remove"},
	} {
		t.Run(pair[0]+" then "+pair[1], func(t *testing.T) {
			mountPath := t.TempDir()
			ops := operations(mountPath)
			first, second := ops[pair[0]], ops[pair[1]]
			backend := newFakeBackend("create", "delete")
			d := NewDriver(mountPath, backend, backend)
			first.prepare(backend, "vol0")
			second.prepare(backend, "vol1")

			firstDone := runInBackground(d, first, "vol0")
			backend.waitUntilEntered(t, "vol0")
			secondDone := runInBackground(d, second, "vol1")
			backend.expectNotEntered(t, pair[1]+" of vol1 did not wait for the "+pair[0]+" of vol0")

			close(backend.release)
			if err := waitFor(t, firstDone); err != nil {
				t.Errorf("%s: %v", pair[0], err)
			}
			if err := waitFor(t, secondDone); err != nil {
				t.Errorf("%s: %v", pair[1], err)
			}
		})
	}
}

// Only creating and deleting wait for each other; mounting and unmounting never wait for them in
// the plugin. (stratisd itself may still answer a mount's lookup only once it has finished
// creating a filesystem, but there is no reason to add a wait of our own.)
func TestMountsDoNotWaitForPoolChanges(t *testing.T) {
	for _, change := range []string{"create", "remove"} {
		t.Run(change, func(t *testing.T) {
			mountPath := t.TempDir()
			ops := operations(mountPath)
			op := ops[change]
			backend := newFakeBackend(op.held)
			d := NewDriver(mountPath, backend, backend)
			op.prepare(backend, "changing")
			ops["mount"].prepare(backend, "starting")
			ops["unmount"].prepare(backend, "stopping")

			changeDone := runInBackground(d, op, "changing")
			backend.waitUntilEntered(t, "changing")

			for _, other := range []struct{ op, volume string }{{"mount", "starting"}, {"unmount", "stopping"}} {
				done := runInBackground(d, ops[other.op], other.volume)
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("%s: %v", other.op, err)
					}
				case <-time.After(patience):
					t.Errorf("%s of another volume waited for the %s", other.op, change)
				}
			}

			close(backend.release)
			if err := waitFor(t, changeDone); err != nil {
				t.Errorf("%s: %v", change, err)
			}
		})
	}
}

// Two operations on the same volume must not overlap: the second waits until the first is done,
// and then acts on what the first left behind.
func TestSameVolumeOperationsRunOneAtATime(t *testing.T) {
	tests := []struct {
		first, second string
		// what the second operation should make of the first one's result
		secondErr string
		mounted   bool
		exists    bool
	}{
		// Two containers sharing a volume start at the same moment: it is mounted once, for both.
		{first: "mount", second: "mount", mounted: true, exists: true},
		// A container stops while another one sharing the volume is still starting.
		{first: "mount", second: "unmount", mounted: false, exists: true},
		// A container starts while the last one using the volume is still stopping.
		{first: "unmount", second: "mount", mounted: true, exists: true},
		{first: "create", second: "create", secondErr: "already exists", exists: true},
		{first: "create", second: "mount", mounted: true, exists: true},
		{first: "mount", second: "remove", mounted: false, exists: false},
		{first: "remove", second: "mount", secondErr: "not found", exists: false},
	}

	for _, tt := range tests {
		t.Run(tt.first+" then "+tt.second, func(t *testing.T) {
			mountPath := t.TempDir()
			ops := operations(mountPath)
			first, second := ops[tt.first], ops[tt.second]
			backend := newFakeBackend(first.held)
			d := NewDriver(mountPath, backend, backend)
			const v = "shared"
			first.prepare(backend, v)

			firstDone := runInBackground(d, first, v)
			backend.waitUntilEntered(t, v)
			lookupsSoFar := backend.lookupsOf(v)

			secondDone := runInBackground(d, second, v)

			// The second has to be waiting for the first one's lock, without having touched the
			// volume yet.
			deadline := time.Now().Add(patience)
			for lockUsers(d, v) < 2 {
				if time.Now().After(deadline) {
					close(backend.release)
					t.Fatalf("%s did not wait for %s on the same volume", tt.second, tt.first)
				}
				time.Sleep(time.Millisecond)
			}
			if got := backend.lookupsOf(v); got != lookupsSoFar {
				close(backend.release)
				t.Fatalf("%s looked the volume up while %s was still in progress", tt.second, tt.first)
			}

			close(backend.release)
			if err := waitFor(t, firstDone); err != nil {
				t.Fatalf("%s: %v", tt.first, err)
			}
			err := waitFor(t, secondDone)
			switch {
			case tt.secondErr == "" && err != nil:
				t.Errorf("%s: %v", tt.second, err)
			case tt.secondErr != "" && (err == nil || !strings.Contains(err.Error(), tt.secondErr)):
				t.Errorf("%s: error = %v, want one containing %q", tt.second, err, tt.secondErr)
			}

			if got := backend.isMounted(v); got != tt.mounted {
				t.Errorf("mounted = %v, want %v", got, tt.mounted)
			}
			if got := backend.exists(v); got != tt.exists {
				t.Errorf("exists = %v, want %v", got, tt.exists)
			}
			if n := backend.timesMounted(v); n > 1 {
				t.Errorf("volume mounted %d times, want at most once", n)
			}
			if n := lockedVolumes(d); n != 0 {
				t.Errorf("%d volume locks left behind after every request finished", n)
			}
		})
	}
}

// Get, Path and List take no lock, so podman can ask about a volume while it is being mounted,
// unmounted, created or removed.
func TestLookupsDoNotWaitForLockedOperations(t *testing.T) {
	for _, name := range []string{"create", "remove", "mount", "unmount"} {
		t.Run(name, func(t *testing.T) {
			mountPath := t.TempDir()
			op := operations(mountPath)[name]
			backend := newFakeBackend(op.held)
			d := NewDriver(mountPath, backend, backend)
			const v = "busy"
			op.prepare(backend, v)

			done := runInBackground(d, op, v)
			backend.waitUntilEntered(t, v)
			defer func() {
				close(backend.release)
				_ = waitFor(t, done)
			}()

			lookups := map[string]func() error{
				"get": func() error {
					_, err := d.Get(&volume.GetRequest{Name: v})
					return err
				},
				"path": func() error {
					_, err := d.Path(&volume.PathRequest{Name: v})
					return err
				},
				"list": func() error {
					_, err := d.List()
					return err
				},
			}
			for lookup, call := range lookups {
				answered := make(chan struct{})
				go func() {
					// Their answers depend on how far the operation got; only whether they
					// answer matters here.
					_ = call()
					close(answered)
				}()
				select {
				case <-answered:
				case <-time.After(patience):
					t.Errorf("%s waited for %s to finish", lookup, name)
				}
			}
		})
	}
}

func TestVolumeLocksAreForgottenOnceFree(t *testing.T) {
	var l volumeLocks
	unlockA := l.lock("a")
	unlockB := l.lock("b")
	if len(l.locks) != 2 {
		t.Fatalf("locks = %d, want 2", len(l.locks))
	}
	unlockA()
	unlockB()
	if len(l.locks) != 0 {
		t.Errorf("locks = %d after both were given back, want 0", len(l.locks))
	}

	// A lock that someone is still waiting for is not forgotten when its holder lets go.
	unlock := l.lock("c")
	acquired := make(chan func())
	go func() { acquired <- l.lock("c") }()
	for {
		l.mu.Lock()
		users := l.locks["c"].users
		l.mu.Unlock()
		if users == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	unlock()
	select {
	case unlockAgain := <-acquired:
		unlockAgain()
	case <-time.After(patience):
		t.Fatal("the waiting request never got the lock")
	}
	if len(l.locks) != 0 {
		t.Errorf("locks = %d after every request finished, want 0", len(l.locks))
	}
}
