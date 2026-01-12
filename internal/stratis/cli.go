package stratis

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/kriansa/podman-volume-stratis/internal/log"
)

// CLIManager implements Manager using the stratis CLI
type CLIManager struct {
	pool string
}

// NewCLIManager creates a new Stratis CLI manager for the given pool
func NewCLIManager(pool string) *CLIManager {
	return &CLIManager{
		pool: pool,
	}
}

// stratis runs a stratis command and returns the output
func (m *CLIManager) stratis(args ...string) ([]byte, error) {
	cmd := exec.Command("stratis", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("stratis %s: %w (output: %q)", strings.Join(args, " "), err, string(output))
	}
	return output, nil
}

// PoolExists checks if the configured pool exists
func (m *CLIManager) PoolExists() (bool, error) {
	log.Debug("checking pool exists", "pool", m.pool)

	_, err := m.stratis("pool", "list", "--name", m.pool)
	if err != nil {
		// Check if it's a "pool not found" error
		if strings.Contains(err.Error(), "pool which does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("check pool: %w", err)
	}

	return true, nil
}

// List returns all filesystems in the pool
func (m *CLIManager) List() ([]Filesystem, error) {
	log.Debug("listing filesystems", "pool", m.pool)

	output, err := m.stratis("fs", "list", m.pool)
	if err != nil {
		return nil, fmt.Errorf("list filesystems: %w", err)
	}

	return m.parseFilesystemTable(string(output))
}

// parseFilesystemTable parses the table output from stratis fs list
// Example output:
// Pool          Filesystem   Total / Used / Free / Limit       Device                          UUID
// podman_vols   vol1         1 GiB / 74 MiB / 950 MiB / None   /dev/stratis/podman_vols/vol1   ad719e64-ae83-4997-bf2e-787c7824be0e
func (m *CLIManager) parseFilesystemTable(output string) ([]Filesystem, error) {
	var filesystems []Filesystem

	scanner := bufio.NewScanner(strings.NewReader(output))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip header line
		if lineNum == 1 || strings.TrimSpace(line) == "" {
			continue
		}

		fs, err := m.parseFilesystemTableLine(line)
		if err != nil {
			log.Debug("failed to parse fs line", "line", line, "error", err)
			continue
		}

		filesystems = append(filesystems, *fs)
	}

	return filesystems, nil
}

// parseFilesystemTableLine parses a single line from the table output
func (m *CLIManager) parseFilesystemTableLine(line string) (*Filesystem, error) {
	// The line format is:
	// Pool   Filesystem   Total / Used / Free / Limit   Device   UUID
	// We need to parse this carefully since values have spaces

	// Use regex to extract the parts
	// Pattern: pool  fsname  total / used / free / limit  device  uuid
	pattern := regexp.MustCompile(`^(\S+)\s+(\S+)\s+(.+?)\s+(/dev/\S+)\s+(\S+)\s*$`)
	matches := pattern.FindStringSubmatch(line)
	if matches == nil {
		return nil, fmt.Errorf("line does not match expected format")
	}

	pool := matches[1]
	name := matches[2]
	sizeInfo := matches[3]
	devicePath := matches[4]
	uuid := matches[5]

	// Parse the size info: "1 GiB / 74 MiB / 950 MiB / None"
	total, used, free, sizeLimit, err := parseSizeInfo(sizeInfo)
	if err != nil {
		return nil, fmt.Errorf("parse size info: %w", err)
	}

	return &Filesystem{
		Name:       name,
		Pool:       pool,
		DevicePath: devicePath,
		Total:      total,
		Used:       used,
		Free:       free,
		SizeLimit:  sizeLimit,
		UUID:       uuid,
	}, nil
}

// parseSizeInfo parses the size info string
// Format: "1 GiB / 74 MiB / 950 MiB / None" or "1 GiB / 74 MiB / 950 MiB / 1 GiB"
func parseSizeInfo(s string) (total, used, free uint64, sizeLimit *uint64, err error) {
	parts := strings.Split(s, " / ")
	if len(parts) != 4 {
		return 0, 0, 0, nil, fmt.Errorf("expected 4 parts separated by ' / ', got %d", len(parts))
	}

	total, err = parseSize(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("parse total: %w", err)
	}

	used, err = parseSize(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("parse used: %w", err)
	}

	free, err = parseSize(strings.TrimSpace(parts[2]))
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("parse free: %w", err)
	}

	limitStr := strings.TrimSpace(parts[3])
	if limitStr != "None" {
		limit, err := parseSize(limitStr)
		if err != nil {
			return 0, 0, 0, nil, fmt.Errorf("parse limit: %w", err)
		}
		sizeLimit = &limit
	}

	return total, used, free, sizeLimit, nil
}

// parseSize parses a size string like "1 GiB" or "74 MiB" to bytes
func parseSize(s string) (uint64, error) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected 'value unit' format, got %q", s)
	}

	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse value: %w", err)
	}

	unit := strings.ToUpper(parts[1])
	var multiplier float64
	switch unit {
	case "B":
		multiplier = 1
	case "KIB":
		multiplier = 1024
	case "MIB":
		multiplier = 1024 * 1024
	case "GIB":
		multiplier = 1024 * 1024 * 1024
	case "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "KB":
		multiplier = 1000
	case "MB":
		multiplier = 1000 * 1000
	case "GB":
		multiplier = 1000 * 1000 * 1000
	case "TB":
		multiplier = 1000 * 1000 * 1000 * 1000
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}

	return uint64(value * multiplier), nil
}

// Create creates a new filesystem with the given name and optional size limit
func (m *CLIManager) Create(name string, sizeLimit *uint64) (*Filesystem, error) {
	log.Debug("creating filesystem", "name", name, "pool", m.pool, "sizeLimit", sizeLimit)

	args := []string{"fs", "create"}
	if sizeLimit != nil {
		args = append(args, "--size", formatSize(*sizeLimit))
	}
	args = append(args, m.pool, name)

	if _, err := m.stratis(args...); err != nil {
		return nil, fmt.Errorf("create filesystem: %w", err)
	}

	// Get the created filesystem
	fs, err := m.GetByName(name)
	if err != nil {
		return nil, fmt.Errorf("get created filesystem: %w", err)
	}

	log.Debug("filesystem created", "name", name, "device", fs.DevicePath)
	return fs, nil
}

// formatSize formats a size in bytes to a string suitable for stratis (e.g., "1GiB")
func formatSize(bytes uint64) string {
	// Stratis accepts sizes like "1GiB", "500MiB", etc.
	// We'll use the most appropriate unit
	const (
		gib = 1024 * 1024 * 1024
		mib = 1024 * 1024
		kib = 1024
	)

	if bytes >= gib && bytes%gib == 0 {
		return fmt.Sprintf("%dGiB", bytes/gib)
	}
	if bytes >= mib && bytes%mib == 0 {
		return fmt.Sprintf("%dMiB", bytes/mib)
	}
	if bytes >= kib && bytes%kib == 0 {
		return fmt.Sprintf("%dKiB", bytes/kib)
	}
	return fmt.Sprintf("%dB", bytes)
}

// Delete removes the filesystem with the given name
func (m *CLIManager) Delete(name string) error {
	log.Debug("deleting filesystem", "name", name, "pool", m.pool)

	if _, err := m.stratis("fs", "destroy", m.pool, name); err != nil {
		return fmt.Errorf("delete filesystem: %w", err)
	}

	log.Debug("filesystem deleted", "name", name)
	return nil
}

// GetByName returns the filesystem with the given name
// Returns nil if not found
func (m *CLIManager) GetByName(name string) (*Filesystem, error) {
	log.Debug("getting filesystem by name", "name", name, "pool", m.pool)

	output, err := m.stratis("fs", "list", "--name="+name, m.pool)
	if err != nil {
		// Check if it's a "filesystem not found" error
		if strings.Contains(err.Error(), "filesystem which does not exist") {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get filesystem: %w", err)
	}

	return m.parseDetailedOutput(string(output))
}

// parseDetailedOutput parses the detailed output from stratis fs list --name
// Example:
// UUID: 5944af0c-f520-4773-9006-edcd79a66d50
// Name: vol1
// Pool: podman_vols
//
// Device: /dev/stratis/podman_vols/vol1
//
// Created: Jan 11 2026 01:50
//
// Snapshot origin: None
//
// Sizes:
//
//	Logical size of thin device: 1 GiB
//	Total used (including XFS metadata): 74 MiB
//	Free: 950 MiB
//
//	Size Limit: None
func (m *CLIManager) parseDetailedOutput(output string) (*Filesystem, error) {
	fs := &Filesystem{}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if val, ok := strings.CutPrefix(line, "UUID:"); ok {
			fs.UUID = strings.TrimSpace(val)
		} else if val, ok := strings.CutPrefix(line, "Name:"); ok {
			fs.Name = strings.TrimSpace(val)
		} else if val, ok := strings.CutPrefix(line, "Pool:"); ok {
			fs.Pool = strings.TrimSpace(val)
		} else if val, ok := strings.CutPrefix(line, "Device:"); ok {
			fs.DevicePath = strings.TrimSpace(val)
		} else if val, ok := strings.CutPrefix(line, "Logical size of thin device:"); ok {
			if size, err := parseSize(strings.TrimSpace(val)); err == nil {
				fs.Total = size
			}
		} else if strings.HasPrefix(line, "Total used") {
			// "Total used (including XFS metadata): 74 MiB"
			if _, val, ok := strings.Cut(line, ":"); ok {
				if size, err := parseSize(strings.TrimSpace(val)); err == nil {
					fs.Used = size
				}
			}
		} else if val, ok := strings.CutPrefix(line, "Free:"); ok {
			if size, err := parseSize(strings.TrimSpace(val)); err == nil {
				fs.Free = size
			}
		} else if val, ok := strings.CutPrefix(line, "Size Limit:"); ok {
			if limitStr := strings.TrimSpace(val); limitStr != "None" {
				if limit, err := parseSize(limitStr); err == nil {
					fs.SizeLimit = &limit
				}
			}
		}
	}

	if fs.Name == "" {
		return nil, fmt.Errorf("failed to parse filesystem details")
	}

	return fs, nil
}
