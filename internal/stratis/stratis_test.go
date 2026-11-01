package stratis

import (
	"sync"
	"testing"
	"time"
)

// How long a test waits for something that should happen. Only reached when a test fails.
const patience = 5 * time.Second

// heldManager is a Manager whose Create and Delete announce themselves on entered and then wait
// for release to close, so a test can keep a change in progress for as long as it needs.
type heldManager struct {
	entered chan string
	release chan struct{}
	// panicOn makes Create or Delete of this name panic instead of waiting
	panicOn string

	mu      sync.Mutex
	changed []string
}

func newHeldManager() *heldManager {
	return &heldManager{entered: make(chan string, 16), release: make(chan struct{})}
}

func (m *heldManager) change(name string) {
	if name == m.panicOn {
		panic("backend failed for " + name)
	}
	m.entered <- name
	<-m.release
	m.mu.Lock()
	m.changed = append(m.changed, name)
	m.mu.Unlock()
}

func (m *heldManager) Create(name string, _ *uint64) (*Filesystem, error) {
	m.change(name)
	return &Filesystem{Name: name}, nil
}

func (m *heldManager) Delete(name string) error {
	m.change(name)
	return nil
}

func (m *heldManager) PoolExists() (bool, error)   { return true, nil }
func (m *heldManager) List() ([]Filesystem, error) { return nil, nil }
func (m *heldManager) GetByName(name string) (*Filesystem, error) {
	return &Filesystem{Name: name}, nil
}

func inBackground(f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(patience):
		t.Fatalf("%s did not finish", what)
	}
}

// stratisd stops answering when it is asked to create several filesystems at once, so creates and
// deletes take turns, even for different filesystems.
func TestOneChangeAtATime_ChangesTakeTurns(t *testing.T) {
	changes := map[string]func(m Manager, name string){
		"create": func(m Manager, name string) { _, _ = m.Create(name, nil) },
		"delete": func(m Manager, name string) { _ = m.Delete(name) },
	}
	for _, pair := range [][2]string{{"create", "create"}, {"create", "delete"}, {"delete", "create"}, {"delete", "delete"}} {
		t.Run(pair[0]+" then "+pair[1], func(t *testing.T) {
			backend := newHeldManager()
			m := OneChangeAtATime(backend)

			firstDone := inBackground(func() { changes[pair[0]](m, "fs1") })
			select {
			case <-backend.entered:
			case <-time.After(patience):
				t.Fatalf("the %s never reached stratisd", pair[0])
			}

			secondDone := inBackground(func() { changes[pair[1]](m, "fs2") })
			select {
			case <-backend.entered:
				close(backend.release)
				t.Fatalf("the %s of fs2 reached stratisd while the %s of fs1 was still in progress", pair[1], pair[0])
			case <-time.After(200 * time.Millisecond):
			}

			close(backend.release)
			waitDone(t, firstDone, pair[0])
			waitDone(t, secondDone, pair[1])
		})
	}
}

func TestOneChangeAtATime_LookupsDoNotWait(t *testing.T) {
	backend := newHeldManager()
	m := OneChangeAtATime(backend)

	createDone := inBackground(func() { _, _ = m.Create("fs1", nil) })
	<-backend.entered
	defer func() {
		close(backend.release)
		waitDone(t, createDone, "create")
	}()

	lookups := map[string]func(){
		"GetByName":  func() { _, _ = m.GetByName("fs2") },
		"List":       func() { _, _ = m.List() },
		"PoolExists": func() { _, _ = m.PoolExists() },
	}
	for name, lookup := range lookups {
		select {
		case <-inBackground(lookup):
		case <-time.After(patience):
			t.Errorf("%s waited for a create in progress", name)
		}
	}
}

// The plugin serves requests with net/http, which recovers a panicking request and keeps serving.
// A change that panics must not leave every later create and delete waiting forever.
func TestOneChangeAtATime_PanicReleasesTheLock(t *testing.T) {
	backend := newHeldManager()
	backend.panicOn = "broken"
	close(backend.release)
	m := OneChangeAtATime(backend)

	for _, change := range []struct {
		name string
		run  func()
	}{
		{"create", func() { _, _ = m.Create("broken", nil) }},
		{"delete", func() { _ = m.Delete("broken") }},
	} {
		recovered := func() (r any) {
			defer func() { r = recover() }()
			change.run()
			return nil
		}()
		if recovered == nil {
			t.Fatalf("the %s was expected to panic", change.name)
		}

		var err error
		select {
		case <-inBackground(func() { _, err = m.Create("next", nil) }):
		case <-time.After(patience):
			t.Fatalf("after a %s panicked, the next create waited forever", change.name)
		}
		if err != nil {
			t.Errorf("Create() after a panic: %v", err)
		}
	}
}

// Every Manager the plugin gets comes from NewManager, so that is where the rule has to be applied.
// All backends go through the same wrapping, so the CLI one, which needs no stratisd to construct,
// stands for both.
func TestNewManager_ChangesRunOneAtATime(t *testing.T) {
	m, err := NewManager("pool", "cli")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, ok := m.(*oneChangeAtATime); !ok {
		t.Errorf("NewManager() returned a %T, whose creates and deletes would not run one at a time", m)
	}
}
