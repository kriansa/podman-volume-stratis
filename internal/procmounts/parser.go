package procmounts

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const procMountsPath = "/proc/mounts"

// Parse parses /proc/mounts and returns all mount entries
func Parse() ([]Entry, error) {
	file, err := os.Open(procMountsPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", procMountsPath, err)
	}
	defer file.Close()

	var mounts []Entry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		mounts = append(mounts, Entry{
			Device:     unescapeField(fields[0]),
			MountPoint: unescapeField(fields[1]),
			FSType:     fields[2],
			Options:    fields[3],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", procMountsPath, err)
	}

	return mounts, nil
}

// unescapeField unescapes special characters in mount fields
// /proc/mounts escapes spaces as \040, tabs as \011, etc.
func unescapeField(s string) string {
	s = strings.ReplaceAll(s, "\\040", " ")
	s = strings.ReplaceAll(s, "\\011", "\t")
	s = strings.ReplaceAll(s, "\\012", "\n")
	s = strings.ReplaceAll(s, "\\134", "\\")
	return s
}
