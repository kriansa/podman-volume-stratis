package mount

// Mounter defines the interface for mount/unmount operations
type Mounter interface {
	// Mount mounts the source device to the target directory
	Mount(source, target, fsType string) error
	// Unmount unmounts the target directory
	Unmount(target string) error
	// IsMounted checks if the target is mounted
	IsMounted(target string) (bool, error)
	// GetMountPoint returns the mount point for a source device
	// Returns empty string if not mounted
	GetMountPoint(source string) (string, error)
}
