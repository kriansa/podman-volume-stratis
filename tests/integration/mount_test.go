//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMount_NonExistent(t *testing.T) {
	_, err := testClient.Mount("nonexistent-volume-12345", "test-container-1")
	assert.Error(t, err, "mount nonexistent volume should fail")
}

func TestMount_AlreadyMounted(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create and mount
	createVolume(t, name, nil)

	mountpoint1, err := testClient.Mount(name, "container-1")
	require.NoError(t, err)

	// Mount again with same container ID (should be idempotent)
	mountpoint2, err := testClient.Mount(name, "container-1")
	require.NoError(t, err)
	assert.Equal(t, mountpoint1, mountpoint2, "remount should return same path")

	// Cleanup
	_ = testClient.Unmount(name, "container-1")
}
