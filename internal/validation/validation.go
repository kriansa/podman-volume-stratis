package validation

import (
	"fmt"
	"regexp"
)

const (
	// MinNameLength is the minimum length for a volume name
	MinNameLength = 2
	// MaxNameLength is the maximum length for a volume name
	MaxNameLength = 65
)

// dockerNamePattern matches Docker's naming requirements:
// Must start with alphanumeric, followed by alphanumeric, underscore, dot, or hyphen
// See: https://github.com/moby/moby/blob/master/daemon/names/names.go
var dockerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ValidateVolumeName validates that a volume name meets all requirements:
// - Matches Docker naming pattern (alphanumeric start, alphanumeric/underscore/dot/hyphen continuation)
// - Between 2 and 65 characters
func ValidateVolumeName(name string) error {
	if len(name) < MinNameLength {
		return fmt.Errorf("volume name must be at least %d characters", MinNameLength)
	}

	if len(name) > MaxNameLength {
		return fmt.Errorf("volume name must be at most %d characters", MaxNameLength)
	}

	if !dockerNamePattern.MatchString(name) {
		return fmt.Errorf("volume name must start with alphanumeric and contain only alphanumeric, underscore, dot, or hyphen characters")
	}

	return nil
}
