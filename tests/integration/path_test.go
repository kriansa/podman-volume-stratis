//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPath_NonExistent(t *testing.T) {
	_, err := testClient.Path("nonexistent-volume-12345")
	assert.Error(t, err, "path for nonexistent volume should fail")
}

func TestPath_UnmountedVolume(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create but don't mount
	createVolume(t, name, nil)

	// Path should fail for unmounted volume
	_, err := testClient.Path(name)
	require.Error(t, err, "path should fail for unmounted volume")
}
