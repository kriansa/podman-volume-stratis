//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kriansa/podman-volume-stratis/tests/integration/driverclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests send several requests to the plugin at the same moment, the way podman does when many
// containers start or stop together, such as at boot. Mounts and unmounts of different volumes run
// side by side; requests for the same volume run one at a time; creating and deleting filesystems
// go to stratisd one at a time.

// How long a request may take before the test gives up on it, so that a plugin or stratisd that
// stops answering fails the test instead of hanging the whole run.
const requestTimeout = 60 * time.Second

// request is one call to the plugin.
type request struct {
	method string // such as "VolumeDriver.Mount"
	body   any
	// delay holds this request back after the ones before it were sent, so that it arrives while
	// they are still in progress.
	delay time.Duration
}

// reply is what the plugin answered, and when the answer arrived.
type reply struct {
	Mountpoint string `json:"Mountpoint"`
	Err        string `json:"Err"`
	arrived    time.Time
}

func create(name string) request {
	return request{method: "VolumeDriver.Create", body: driverclient.CreateRequest{Name: name}}
}

func remove(name string) request {
	return request{method: "VolumeDriver.Remove", body: driverclient.RemoveRequest{Name: name}}
}

func mount(name, container string) request {
	return request{method: "VolumeDriver.Mount", body: driverclient.MountRequest{Name: name, ID: container}}
}

func unmount(name, container string) request {
	return request{method: "VolumeDriver.Unmount", body: driverclient.UnmountRequest{Name: name, ID: container}}
}

// sendAtOnce sends every request at the same moment and waits for all the replies. testClient
// cannot do this: it runs one command in the VM per request, one after another, so the requests
// are sent from a single command in the VM instead.
func sendAtOnce(t *testing.T, requests ...request) []reply {
	t.Helper()
	dir := fmt.Sprintf("/tmp/requests-%d", time.Now().UnixNano())

	var script strings.Builder
	fmt.Fprintf(&script, "mkdir -p %s; ", dir)
	for i, r := range requests {
		body, err := json.Marshal(r.body)
		require.NoError(t, err)
		if r.delay > 0 {
			fmt.Fprintf(&script, "sleep %.3f; ", r.delay.Seconds())
		}
		fmt.Fprintf(&script,
			`( sudo curl -s --max-time %d --unix-socket %s -X POST -H "Content-Type: application/json" `+
				`-d '%s' http://localhost/%s > %s/%d.json; date +%%s%%N > %s/%d.arrived ) & `,
			int(requestTimeout.Seconds()), socketPath, body, r.method, dir, i, dir, i)
	}
	script.WriteString("wait")

	output, err := testVM.Run(script.String())
	require.NoError(t, err, output)
	defer func() { _, _ = testVM.Run("rm -rf " + dir) }()

	replies := make([]reply, len(requests))
	for i := range requests {
		output, err := testVM.Run(fmt.Sprintf("cat %s/%d.json", dir, i))
		require.NoError(t, err, output)
		require.NotEmpty(t, output, "%s %s got no answer within %v", requests[i].method, requests[i].body, requestTimeout)
		require.NoError(t, json.Unmarshal([]byte(output), &replies[i]), output)

		output, err = testVM.Run(fmt.Sprintf("cat %s/%d.arrived", dir, i))
		require.NoError(t, err, output)
		nanos, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
		require.NoError(t, err, output)
		replies[i].arrived = time.Unix(0, nanos)
	}
	return replies
}

// timesMounted counts the entries for a mount point in the VM's mount table.
func timesMounted(t *testing.T, mountPoint string) int {
	t.Helper()
	output, err := testVM.Run(fmt.Sprintf("grep -c ' %s ' /proc/mounts || true", mountPoint))
	require.NoError(t, err, output)
	n, err := strconv.Atoi(strings.TrimSpace(output))
	require.NoError(t, err, output)
	return n
}

func volumeNames(t *testing.T, count int) []string {
	base := uniqueVolumeName(t)
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("%s-%d", base, i)
	}
	return names
}

// stratisd 3.9 stops answering altogether when it is asked to create several filesystems at once,
// which is why the plugin sends it creates one at a time. Sent all at once here, the requests have
// to come back, every one of them successful.
func TestConcurrent_CreateDifferentVolumes(t *testing.T) {
	names := volumeNames(t, 5)
	var requests []request
	for _, name := range names {
		cleanupVolume(t, name)
		requests = append(requests, create(name))
	}

	for i, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err, "create %s", names[i])
	}
	for _, name := range names {
		assertVolumeNotMounted(t, name)
	}
}

func TestConcurrent_CreateSameVolume(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	created := 0
	for _, r := range sendAtOnce(t, create(name), create(name), create(name), create(name)) {
		if r.Err == "" {
			created++
		} else {
			assert.Contains(t, r.Err, "already exists")
		}
	}
	assert.Equal(t, 1, created, "exactly one of the requests should create the volume")
	assertVolumeExists(t, name)
}

func TestConcurrent_MountDifferentVolumes(t *testing.T) {
	names := volumeNames(t, 5)
	var requests []request
	for _, name := range names {
		createVolume(t, name, nil)
		requests = append(requests, mount(name, "container-"+name))
	}

	for i, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err, "mount %s", names[i])
		assert.Equal(t, expectedMountPath(names[i]), r.Mountpoint)
		assertVolumeMounted(t, names[i], expectedMountPath(names[i]))
	}
}

// Containers sharing a volume, starting together: it is mounted once, for all of them.
func TestConcurrent_MountSameVolume(t *testing.T) {
	name := uniqueVolumeName(t)
	createVolume(t, name, nil)

	var requests []request
	for i := range 5 {
		requests = append(requests, mount(name, fmt.Sprintf("container-%d", i)))
	}
	for _, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err)
		assert.Equal(t, expectedMountPath(name), r.Mountpoint)
	}
	assert.Equal(t, 1, timesMounted(t, expectedMountPath(name)), "the volume should be mounted once")
}

func TestConcurrent_UnmountDifferentVolumes(t *testing.T) {
	names := volumeNames(t, 5)
	var requests []request
	for _, name := range names {
		createVolume(t, name, nil)
		_, err := testClient.Mount(name, "container-"+name)
		require.NoError(t, err)
		requests = append(requests, unmount(name, "container-"+name))
	}

	for i, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err, "unmount %s", names[i])
		assertVolumeNotMounted(t, names[i])
	}
}

// A container stopping while another one sharing the volume starts: whichever runs last decides,
// and the plugin's answer has to match what is actually mounted.
func TestConcurrent_MountAndUnmountSameVolume(t *testing.T) {
	name := uniqueVolumeName(t)
	createVolume(t, name, nil)
	_, err := testClient.Mount(name, "stopping")
	require.NoError(t, err)

	for _, r := range sendAtOnce(t, unmount(name, "stopping"), mount(name, "starting")) {
		require.Empty(t, r.Err)
	}

	mounts := timesMounted(t, expectedMountPath(name))
	require.LessOrEqual(t, mounts, 1, "the volume should be mounted at most once")
	vol := assertVolumeExists(t, name)
	if mounts == 1 {
		assert.Equal(t, expectedMountPath(name), vol.Mountpoint)
	} else {
		assert.Empty(t, vol.Mountpoint)
	}
	_ = testClient.Unmount(name, "starting")
}

func TestConcurrent_RemoveDifferentVolumes(t *testing.T) {
	names := volumeNames(t, 5)
	var requests []request
	for _, name := range names {
		createVolume(t, name, nil)
		requests = append(requests, remove(name))
	}

	for i, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err, "remove %s", names[i])
		assertVolumeNotExists(t, names[i])
	}
}

func TestConcurrent_CreateAndRemoveDifferentVolumes(t *testing.T) {
	removing := volumeNames(t, 3)
	creating := make([]string, len(removing))
	var requests []request
	for i, name := range removing {
		createVolume(t, name, nil)
		creating[i] = name + "-new"
		cleanupVolume(t, creating[i])
		requests = append(requests, remove(name), create(creating[i]))
	}

	for i, r := range sendAtOnce(t, requests...) {
		require.Empty(t, r.Err, "%s %v", requests[i].method, requests[i].body)
	}
	for i := range removing {
		assertVolumeNotExists(t, removing[i])
		assertVolumeExists(t, creating[i])
	}
}

// Whichever runs first, the volume ends up removed and nothing is left mounted.
func TestConcurrent_RemoveAndMountSameVolume(t *testing.T) {
	name := uniqueVolumeName(t)
	createVolume(t, name, nil)

	replies := sendAtOnce(t, remove(name), mount(name, "container"))
	require.Empty(t, replies[0].Err, "remove should succeed")
	if replies[1].Err != "" {
		assert.Contains(t, replies[1].Err, "not found", "a mount after the removal finds nothing")
	}

	assertVolumeNotExists(t, name)
	assert.Equal(t, 0, timesMounted(t, expectedMountPath(name)))
}

// The reason requests for different volumes run side by side: one that takes long must not hold
// up others. On a real disk mounting takes about a second, and podman gives up on a request after
// five, so with a single queue for every volume the last containers to start failed.
//
// Here the slow mount is made slow on purpose: its device is suspended, so the mount waits until
// the device is resumed. A mount of another volume sent meanwhile has to be answered first.
// (--nolockfs, because a plain suspend also freezes the device, and the kernel refuses to mount a
// frozen device at once instead of waiting.)
//
// (Creating a filesystem would not do as the slow request: stratisd itself answers nothing else
// until it has finished, so a mount waits for a create whatever the plugin does.)
func TestConcurrent_SlowMountDoesNotHoldUpOtherVolumes(t *testing.T) {
	names := volumeNames(t, 2)
	slow, quick := names[0], names[1]
	createVolume(t, slow, nil)
	createVolume(t, quick, nil)

	output, err := testVM.Run(fmt.Sprintf("lsblk -dno NAME /dev/stratis/%s/%s", stratisPoolName, slow))
	require.NoError(t, err, output)
	device := strings.TrimSpace(output)
	require.NotEmpty(t, device)

	resume := "sudo dmsetup resume " + device
	t.Cleanup(func() { _, _ = testVM.Run(resume + " 2>/dev/null") })
	output, err = testVM.Run("sudo dmsetup suspend --nolockfs " + device)
	require.NoError(t, err, output)
	// Resumed a few seconds from now, by itself, so that the slow mount can finish.
	output, err = testVM.Run(fmt.Sprintf("nohup sh -c 'sleep 4; %s' >/dev/null 2>&1 &", resume))
	require.NoError(t, err, output)

	replies := sendAtOnce(t,
		mount(slow, "container-slow"),
		// Sent once the slow mount is under way.
		withDelay(mount(quick, "container-quick"), 300*time.Millisecond),
	)
	require.Empty(t, replies[0].Err, "the slow mount should succeed once its device is resumed")
	require.Empty(t, replies[1].Err, "the quick mount should succeed")

	t.Logf("slow mount answered %v after the quick one", replies[0].arrived.Sub(replies[1].arrived).Round(time.Millisecond))
	assert.True(t, replies[1].arrived.Before(replies[0].arrived),
		"the mount of %s should not wait for the mount of %s", quick, slow)
}

func withDelay(r request, delay time.Duration) request {
	r.delay = delay
	return r
}
