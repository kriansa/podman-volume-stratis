//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVolumeLifecycle tests the complete volume lifecycle:
// create -> list (verify exists) -> get -> mount -> get (verify mounted) ->
// path -> unmount -> get (verify unmounted) -> delete -> list (verify gone) -> get (verify error)
func TestVolumeLifecycle(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Step 1: Create volume
	t.Run("step1_create", func(t *testing.T) {
		err := testClient.Create(name, nil)
		require.NoError(t, err, "create should succeed")
	})

	// Step 2: Verify volume exists via List
	t.Run("step2_list_after_create", func(t *testing.T) {
		assertVolumeInList(t, name)
	})

	// Step 3: Verify volume exists via Get
	t.Run("step3_get_after_create", func(t *testing.T) {
		vol := assertVolumeExists(t, name)
		require.Empty(t, vol.Mountpoint, "unmounted volume should have empty mountpoint")
	})

	// Step 4: Mount volume
	var mountpoint string
	t.Run("step4_mount", func(t *testing.T) {
		var err error
		mountpoint, err = testClient.Mount(name, "test-container-1")
		require.NoError(t, err, "mount should succeed")
		require.Equal(t, expectedMountPath(name), mountpoint, "mountpoint should match expected")
	})

	// Step 5: Verify mounted via Get
	t.Run("step5_get_after_mount", func(t *testing.T) {
		assertVolumeMounted(t, name, mountpoint)
	})

	// Step 6: Verify mounted via List
	t.Run("step6_list_after_mount", func(t *testing.T) {
		vol := assertVolumeInList(t, name)
		require.Equal(t, mountpoint, vol.Mountpoint, "list should show mountpoint")
	})

	// Step 7: Verify Path returns mountpoint
	t.Run("step7_path_while_mounted", func(t *testing.T) {
		path, err := testClient.Path(name)
		require.NoError(t, err, "path should succeed")
		require.Equal(t, mountpoint, path, "path should match mountpoint")
	})

	// Step 8: Unmount volume
	t.Run("step8_unmount", func(t *testing.T) {
		err := testClient.Unmount(name, "test-container-1")
		require.NoError(t, err, "unmount should succeed")
	})

	// Step 9: Verify unmounted via Get
	t.Run("step9_get_after_unmount", func(t *testing.T) {
		assertVolumeNotMounted(t, name)
	})

	// Step 10: Delete volume
	t.Run("step10_delete", func(t *testing.T) {
		err := testClient.Remove(name)
		require.NoError(t, err, "remove should succeed")
	})

	// Step 11: Verify not in List after delete
	t.Run("step11_list_after_delete", func(t *testing.T) {
		assertVolumeNotInList(t, name)
	})

	// Step 12: Verify Get fails after delete
	t.Run("step12_get_after_delete", func(t *testing.T) {
		assertVolumeNotExists(t, name)
	})
}

// TestVolumeLifecycle_WithSize tests lifecycle with size option
func TestVolumeLifecycle_WithSize(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create with size option
	t.Run("step1_create_with_size", func(t *testing.T) {
		opts := map[string]string{"size": "1GiB"}
		err := testClient.Create(name, opts)
		require.NoError(t, err, "create with size should succeed")
	})

	// Verify exists
	t.Run("step2_verify_exists", func(t *testing.T) {
		assertVolumeExists(t, name)
	})

	// Delete
	t.Run("step3_delete", func(t *testing.T) {
		err := testClient.Remove(name)
		require.NoError(t, err, "remove should succeed")
	})
}

// TestVolumeLifecycle_MountRemount tests mount/unmount/remount cycle
func TestVolumeLifecycle_MountRemount(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create
	createVolume(t, name, nil)

	// Mount
	t.Run("step1_first_mount", func(t *testing.T) {
		_, err := testClient.Mount(name, "container-1")
		require.NoError(t, err)
	})

	// Unmount
	t.Run("step2_unmount", func(t *testing.T) {
		err := testClient.Unmount(name, "container-1")
		require.NoError(t, err)
	})

	// Verify unmounted
	t.Run("step3_verify_unmounted", func(t *testing.T) {
		assertVolumeNotMounted(t, name)
	})

	// Remount
	t.Run("step4_remount", func(t *testing.T) {
		mountpoint, err := testClient.Mount(name, "container-2")
		require.NoError(t, err)
		require.Equal(t, expectedMountPath(name), mountpoint)
	})

	// Verify mounted again
	t.Run("step5_verify_remounted", func(t *testing.T) {
		assertVolumeMounted(t, name, expectedMountPath(name))
	})

	// Cleanup
	_ = testClient.Unmount(name, "container-2")
}

// TestVolumeLifecycle_DataPersistence tests that data persists across unmount/remount
func TestVolumeLifecycle_DataPersistence(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create and mount
	createVolume(t, name, nil)

	mountpoint, err := testClient.Mount(name, "container-1")
	require.NoError(t, err)

	// Write data
	testData := "Hello from integration test!"
	testFile := fmt.Sprintf("%s/test.txt", mountpoint)
	t.Run("step1_write_data", func(t *testing.T) {
		_, err := testVM.Run(fmt.Sprintf("echo '%s' | sudo tee %s", testData, testFile))
		require.NoError(t, err, "write to mounted volume should succeed")
	})

	// Verify data is readable
	t.Run("step2_read_data", func(t *testing.T) {
		output, err := testVM.Run(fmt.Sprintf("cat %s", testFile))
		require.NoError(t, err)
		require.Contains(t, output, testData)
	})

	// Unmount
	t.Run("step3_unmount", func(t *testing.T) {
		err := testClient.Unmount(name, "container-1")
		require.NoError(t, err)
	})

	// Remount
	t.Run("step4_remount", func(t *testing.T) {
		_, err := testClient.Mount(name, "container-2")
		require.NoError(t, err)
	})

	// Verify data persisted
	t.Run("step5_verify_data_persisted", func(t *testing.T) {
		output, err := testVM.Run(fmt.Sprintf("cat %s", testFile))
		require.NoError(t, err)
		require.Contains(t, output, testData, "data should persist after unmount/remount")
	})

	// Cleanup
	_ = testClient.Unmount(name, "container-2")
}

// TestVolumeLifecycle_MultipleVolumes tests creating and listing multiple volumes
func TestVolumeLifecycle_MultipleVolumes(t *testing.T) {
	var names []string
	for range 3 {
		name := uniqueVolumeName(t)
		names = append(names, name)
		cleanupVolume(t, name)
	}

	// Create all
	t.Run("step1_create_all", func(t *testing.T) {
		for _, name := range names {
			err := testClient.Create(name, nil)
			require.NoError(t, err, "create %s should succeed", name)
		}
	})

	// Verify all in list
	t.Run("step2_verify_all_in_list", func(t *testing.T) {
		for _, name := range names {
			assertVolumeInList(t, name)
		}
	})

	// Delete all
	t.Run("step3_delete_all", func(t *testing.T) {
		for _, name := range names {
			err := testClient.Remove(name)
			require.NoError(t, err, "remove %s should succeed", name)
		}
	})

	// Verify all gone
	t.Run("step4_verify_all_gone", func(t *testing.T) {
		for _, name := range names {
			assertVolumeNotInList(t, name)
		}
	})
}
