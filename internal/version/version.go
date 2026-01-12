package version

import (
	"fmt"
	"runtime"
)

// Set via ldflags at build time
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (commit: %s, built: %s, go: %s)",
		Version, Commit, BuildTime, runtime.Version())
}
