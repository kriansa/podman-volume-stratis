package driver

import "sync"

// volumeLocks gives every volume a lock of its own.
//
// Requests for the same volume run one at a time, so none of them sees another one's mount or
// removal half done. Requests for different volumes run side by side. That matters when many
// containers start at once, such as at boot: podman gives up on a request after a few seconds, and
// with a single lock for everything, the last volumes in the queue waited for every mount ahead of
// them, so their containers failed to start. (Creating and deleting still wait for each other at
// stratisd: see Driver.poolChanges.)
type volumeLocks struct {
	mu    sync.Mutex
	locks map[string]*volumeLock
}

type volumeLock struct {
	mu sync.Mutex
	// users counts the requests holding this lock or waiting for it. The lock is forgotten once
	// nobody needs it, so removed volumes do not leave locks behind.
	users int
}

// lock waits until no other request holds the named volume, takes it, and returns the function
// that gives it back.
func (l *volumeLocks) lock(name string) (unlock func()) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*volumeLock)
	}
	vl := l.locks[name]
	if vl == nil {
		vl = &volumeLock{}
		l.locks[name] = vl
	}
	vl.users++
	l.mu.Unlock()

	vl.mu.Lock()

	return func() {
		vl.mu.Unlock()

		l.mu.Lock()
		vl.users--
		if vl.users == 0 {
			delete(l.locks, name)
		}
		l.mu.Unlock()
	}
}
