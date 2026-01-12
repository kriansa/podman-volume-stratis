//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/kriansa/podman-volume-stratis/tests/integration/driverclient"
	"github.com/stretchr/testify/require"
)

// uniqueVolumeName generates a unique volume name for a test
func uniqueVolumeName(t *testing.T) string {
	return fmt.Sprintf("test-%s-%d", t.Name(), time.Now().UnixNano()%10000)
}

// cleanupVolume registers cleanup for a volume at test end
func cleanupVolume(t *testing.T, name string) {
	t.Cleanup(func() {
		// Unmount if mounted
		_, _ = testVM.Run(fmt.Sprintf("sudo umount %s/%s 2>/dev/null || true", mountBasePath, name))
		// Destroy filesystem
		_, _ = testVM.Run(fmt.Sprintf("sudo stratis fs destroy %s %s 2>/dev/null || true", stratisPoolName, name))
	})
}

// assertVolumeExists verifies a volume exists using Get
func assertVolumeExists(t *testing.T, name string) *driverclient.Volume {
	t.Helper()
	vol, err := testClient.Get(name)
	require.NoError(t, err, "volume %s should exist", name)
	require.NotNil(t, vol, "volume %s should not be nil", name)
	require.Equal(t, name, vol.Name, "volume name should match")
	return vol
}

// assertVolumeNotExists verifies a volume does not exist using Get
func assertVolumeNotExists(t *testing.T, name string) {
	t.Helper()
	_, err := testClient.Get(name)
	require.Error(t, err, "volume %s should not exist", name)
}

// assertVolumeInList verifies a volume appears in List
func assertVolumeInList(t *testing.T, name string) *driverclient.Volume {
	t.Helper()
	volumes, err := testClient.List()
	require.NoError(t, err, "list should succeed")

	for _, v := range volumes {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("volume %s not found in list", name)
	return nil
}

// assertVolumeNotInList verifies a volume does not appear in List
func assertVolumeNotInList(t *testing.T, name string) {
	t.Helper()
	volumes, err := testClient.List()
	require.NoError(t, err, "list should succeed")

	for _, v := range volumes {
		if v.Name == name {
			t.Fatalf("volume %s should not be in list", name)
		}
	}
}

// assertVolumeMounted verifies a volume is mounted at expected path using Get
func assertVolumeMounted(t *testing.T, name string, expectedPath string) {
	t.Helper()
	vol := assertVolumeExists(t, name)
	require.Equal(t, expectedPath, vol.Mountpoint, "volume should be mounted at %s", expectedPath)
}

// assertVolumeNotMounted verifies a volume is not mounted using Get
func assertVolumeNotMounted(t *testing.T, name string) {
	t.Helper()
	vol := assertVolumeExists(t, name)
	require.Empty(t, vol.Mountpoint, "volume should not be mounted")
}

// createVolume is a helper that creates a volume and registers cleanup
func createVolume(t *testing.T, name string, opts map[string]string) {
	t.Helper()
	cleanupVolume(t, name)
	err := testClient.Create(name, opts)
	require.NoError(t, err, "create volume %s should succeed", name)
}

// expectedMountPath returns the expected mount path for a volume
func expectedMountPath(name string) string {
	return fmt.Sprintf("%s/%s", mountBasePath, name)
}
