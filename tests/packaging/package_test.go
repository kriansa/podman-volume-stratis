//go:build integration

package packaging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriansa/podman-volume-stratis/tests/integration/log"
	"github.com/kriansa/podman-volume-stratis/tests/integration/vm"
)

const (
	plugin  = "podman-volume-stratis"
	watcher = "stratisd-fix-ordering.path"
	// Two units standing in for quadlet volumes: one depends on the plugin with Requires=, the
	// other with Wants=, as the README recommends.
	requiresPlugin = "requires-plugin"
	wantsPlugin    = "wants-plugin"
	// A volume mounted through the plugin, to check it stays mounted while the plugin restarts
	keptMounted = "kept-mounted"
	socketPath  = "/run/podman/plugins/volume-stratis.sock"
)

var testVM vm.VM

// TestMain starts a VM of its own rather than sharing the one in tests/integration. The package
// installs a service with the same name as the plugin those tests run, and removing the package
// stops and disables it.
func TestMain(m *testing.M) {
	rpmPath, oldRPMPath := os.Getenv("PLUGIN_RPM"), os.Getenv("PLUGIN_RPM_OLD")
	if rpmPath == "" || oldRPMPath == "" {
		log.Status("PLUGIN_RPM must point at the x86_64 RPM to test, and PLUGIN_RPM_OLD at 1.1.4's. " +
			"Run 'make test-integration'.")
		os.Exit(1)
	}

	// Stop the VM when interrupted, as on a cancelled CI job, rather than leave it holding its
	// SSH port and snapshot for the next run to trip over.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		log.Status("\nInterrupted, shutting down...")
		if testVM != nil {
			testVM.Stop()
		}
		os.Exit(1)
	}()

	ctx := context.Background()
	qemuVM, err := vm.StartQEMUVM(ctx)
	if err != nil {
		log.Status("Failed to start VM: %v", err)
		os.Exit(1)
	}
	testVM = qemuVM

	code := 1
	if err := setupVM(ctx, rpmPath, oldRPMPath); err != nil {
		log.Status("Failed to set up the VM: %v", err)
	} else {
		code = m.Run()
	}
	testVM.Stop()
	os.Exit(code)
}

func setupVM(ctx context.Context, rpmPath, oldRPMPath string) error {
	if err := testVM.WaitForSSH(ctx); err != nil {
		return fmt.Errorf("wait for SSH: %w", err)
	}
	if err := waitUntil(30*time.Second, func() bool { return activeState("stratisd") == "active" }); err != nil {
		return fmt.Errorf("wait for stratisd: %w", err)
	}
	unit := `printf '[Unit]\n%[1]s=%[2]s.service\nAfter=%[2]s.service\n[Service]\nExecStart=/usr/bin/sleep infinity\n' ` +
		`| sudo tee /etc/systemd/system/%[3]s.service >/dev/null`
	steps := []string{
		"sudo stratis pool create test_pool /dev/loop0",
		// The image comes with a build of the plugin under the same service name, from a unit in
		// /etc that would take precedence over the package's own.
		"sudo systemctl disable --now " + plugin,
		"sudo rm -f /etc/systemd/system/" + plugin + ".service",
		fmt.Sprintf(unit, "Requires", plugin, requiresPlugin),
		fmt.Sprintf(unit, "Wants", plugin, wantsPlugin),
		"sudo systemctl daemon-reload",
	}
	for _, step := range steps {
		if output, err := testVM.Run(step); err != nil {
			return fmt.Errorf("%s: %w: %s", step, err, output)
		}
	}
	if err := testVM.CopyFile(rpmPath, "/tmp/plugin.rpm"); err != nil {
		return fmt.Errorf("copy %s to the VM: %w", rpmPath, err)
	}
	if err := testVM.CopyFile(oldRPMPath, "/tmp/plugin-1.1.4.rpm"); err != nil {
		return fmt.Errorf("copy %s to the VM: %w", oldRPMPath, err)
	}
	return nil
}

func waitUntil(timeout time.Duration, ready func() bool) error {
	deadline := time.Now().Add(timeout)
	for !ready() {
		if time.Now().After(deadline) {
			return fmt.Errorf("not ready after %v", timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func unitProperty(unit, property string) string {
	output, _ := testVM.Run(fmt.Sprintf("systemctl show -p %s --value %s", property, unit))
	return strings.TrimSpace(output)
}

func activeState(unit string) string { return unitProperty(unit, "ActiveState") }

// invocation identifies one run of a unit: it changes every time the unit starts.
func invocation(unit string) string { return unitProperty(unit, "InvocationID") }

func isEnabled(unit string) bool {
	output, _ := testVM.Run("systemctl is-enabled " + unit)
	return strings.TrimSpace(output) == "enabled"
}

func overrideInstalled() bool {
	_, err := testVM.Run("test -f /etc/systemd/system/stratisd.service")
	return err == nil
}

func isMounted(volume string) bool {
	_, err := testVM.Run("findmnt -n /mnt/" + volume)
	return err == nil
}

func run(t *testing.T, cmd string) {
	t.Helper()
	output, err := testVM.Run(cmd)
	require.NoError(t, err, "%s\n%s", cmd, output)
}

// callPlugin sends one request to the plugin's socket and fails the test unless it succeeds.
func callPlugin(t *testing.T, method, body string) {
	t.Helper()
	output, err := testVM.Run(fmt.Sprintf(
		`sudo curl -sS --max-time 60 --unix-socket %s -X POST -d '%s' http://localhost/VolumeDriver.%s`,
		socketPath, body, method))
	require.NoError(t, err, output)
	var reply struct{ Err string }
	require.NoError(t, json.Unmarshal([]byte(output), &reply), "%s: %s", method, output)
	require.Empty(t, reply.Err, "%s should succeed", method)
}

// startPlugin points the installed package at the test pool and starts the plugin, as the README
// says, and the two units that depend on it.
func startPlugin(t *testing.T) {
	t.Helper()
	run(t, `sudo sed -i 's/^pool = .*/pool = "test_pool"/' /etc/containers/plugin-volume-stratis.conf`)
	run(t, fmt.Sprintf("sudo systemctl enable --now %s && sudo systemctl start %s %s", plugin, requiresPlugin, wantsPlugin))
	require.NoError(t, waitUntil(30*time.Second, func() bool {
		return activeState(plugin) == "active" && activeState(requiresPlugin) == "active" &&
			activeState(wantsPlugin) == "active"
	}), "the plugin and the units that depend on it should start")
}

// Install, upgrade and removal, in that order, through rpm itself, so that rpm decides which
// scripts run and with which arguments.
func TestPackageLifecycle(t *testing.T) {
	// So that a failure halfway does not leave the package installed for the next test
	t.Cleanup(func() {
		_, _ = testVM.Run("sudo rpm -e --nodeps " + plugin)
		_, _ = testVM.Run("sudo umount /mnt/" + keptMounted + "; sudo stratis fs destroy test_pool " + keptMounted)
	})

	if !t.Run("install", func(t *testing.T) {
		run(t, "sudo rpm -i --nodeps /tmp/plugin.rpm")

		// Installing does not start the plugin: its pool has to be configured first.
		assert.Equal(t, "inactive", activeState(plugin))
		assert.True(t, overrideInstalled(), "the stratisd override should be installed")
		assert.True(t, isEnabled(watcher), "the stratisd override's path watcher should be enabled")
	}) {
		return
	}

	startPlugin(t)
	callPlugin(t, "Create", fmt.Sprintf(`{"Name":"%s"}`, keptMounted))
	callPlugin(t, "Mount", fmt.Sprintf(`{"Name":"%s","ID":"container"}`, keptMounted))
	require.True(t, isMounted(keptMounted), "the volume should be mounted before the upgrade")

	if !t.Run("upgrade", func(t *testing.T) {
		pluginBefore := invocation(plugin)
		requiresBefore, wantsBefore := invocation(requiresPlugin), invocation(wantsPlugin)

		// Reinstalling runs the scripts as an upgrade does: the new copy's posttrans, and the old
		// copy's preremove and postremove with 1, as a copy remains.
		run(t, "sudo rpm -Uvh --replacepkgs --nodeps /tmp/plugin.rpm")

		// The restart is not waited for, so give it a moment.
		err := waitUntil(60*time.Second, func() bool {
			return activeState(plugin) == "active" && invocation(plugin) != pluginBefore &&
				activeState(requiresPlugin) == "active" && invocation(requiresPlugin) != requiresBefore
		})
		assert.NoError(t, err, "the plugin and the unit that requires it should both have restarted: "+
			"plugin %s, %s %s", activeState(plugin), requiresPlugin, activeState(requiresPlugin))
		assert.True(t, isEnabled(plugin), "the plugin should still be enabled")
		assert.True(t, overrideInstalled(), "the stratisd override should still be installed")
		assert.True(t, isEnabled(watcher), "the path watcher should still be enabled")

		// What the README says to expect with Wants= instead of Requires=
		assert.Equal(t, "active", activeState(wantsPlugin), "a unit that only wants the plugin keeps running")
		assert.Equal(t, wantsBefore, invocation(wantsPlugin), "a unit that only wants the plugin is not restarted")
		assert.True(t, isMounted(keptMounted), "a mounted volume stays mounted while the plugin restarts")
	}) {
		return
	}

	t.Run("remove", func(t *testing.T) {
		run(t, "sudo rpm -e --nodeps "+plugin)

		assert.NoError(t, waitUntil(30*time.Second, func() bool { return activeState(plugin) == "inactive" }),
			"the plugin should be stopped")
		assert.False(t, isEnabled(plugin), "the plugin should no longer be enabled")
		assert.Equal(t, "not-found", unitProperty(plugin, "LoadState"),
			"systemd should have reloaded once the unit file was gone")
		assert.Equal(t, "inactive", activeState(requiresPlugin), "a unit that requires the plugin stops with it")
		assert.False(t, overrideInstalled(), "the stratisd override should be removed")
		assert.False(t, isEnabled(watcher), "the path watcher should no longer be enabled")
	})
}

// gaia and atlas run 1.1.3, whose scripts are the same as 1.1.4's. Upgrading from those still runs
// the old removal script, which stops and disables the plugin and removes the stratisd override.
// The new version's posttrans runs after it and puts the override back. Nothing in the new package
// can stop the old script from stopping the plugin, but units that only want the plugin are left
// alone.
func TestUpgradeFromAnOldRelease(t *testing.T) {
	run(t, "sudo rpm -i --nodeps /tmp/plugin-1.1.4.rpm")
	t.Cleanup(func() { _, _ = testVM.Run("sudo rpm -e --nodeps " + plugin) })
	startPlugin(t)

	// The test build's version sorts below 1.1.4 (1.1.4~SNAPSHOT), hence --oldpackage. rpm runs the
	// scripts exactly as for any upgrade.
	run(t, "sudo rpm -Uvh --oldpackage --nodeps /tmp/plugin.rpm")

	assert.True(t, overrideInstalled(), "posttrans should have put the stratisd override back")
	assert.True(t, isEnabled(watcher), "posttrans should have enabled the path watcher again")

	assert.Equal(t, "inactive", activeState(plugin), "1.1.4's removal script stops the plugin")
	assert.False(t, isEnabled(plugin), "1.1.4's removal script disables the plugin")
	assert.Equal(t, "inactive", activeState(requiresPlugin), "a unit that requires the plugin stops with it")
	assert.Equal(t, "active", activeState(wantsPlugin), "a unit that only wants the plugin keeps running")
}
