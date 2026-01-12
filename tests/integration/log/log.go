//go:build integration

package log

import (
	"fmt"
	"os"
)

// Status prints a status message for immediate display during tests.
func Status(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stdout, format+"\n", args...)
}
