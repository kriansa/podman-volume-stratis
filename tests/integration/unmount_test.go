//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmount_NonExistent(t *testing.T) {
	err := testClient.Unmount("nonexistent-volume-12345", "test-container-1")
	assert.Error(t, err, "unmount nonexistent volume should fail")
}

func TestUnmount_NotMounted(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create but don't mount
	createVolume(t, name, nil)

	// Unmount should succeed (idempotent - driver returns nil if not mounted)
	err := testClient.Unmount(name, "test-container-1")
	require.NoError(t, err, "unmount not-mounted should be idempotent")
}
