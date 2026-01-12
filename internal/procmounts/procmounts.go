package procmounts

// Entry represents an entry in /proc/mounts
type Entry struct {
	Device     string
	MountPoint string
	FSType     string
	Options    string
}
