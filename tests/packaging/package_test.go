//go:build integration

package packaging

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriansa/podman-volume-stratis/tests/integration/log"
	"github.com/kriansa/podman-volume-stratis/tests/integration/vm"
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
		return fmt.Errorf("stratisd: %w", err)
	}
	steps := []string{
		"sudo stratis pool create test_pool /dev/loop0",
		// The image comes with a build of the plugin under the same service name, from a unit in
		// /etc that would take precedence over the package's own.
		"sudo systemctl disable --now podman-volume-stratis",
		"sudo rm -f /etc/systemd/system/podman-volume-stratis.service",
		"sudo systemctl daemon-reload",
		// Stands in for a quadlet volume, whose unit requires the plugin's service
		fmt.Sprintf(`printf '[Unit]\nRequires=%[1]s.service\nAfter=%[1]s.service\n[Service]\nExecStart=/usr/bin/sleep infinity\n' `+
			`| sudo tee /etc/systemd/system/%[2]s.service >/dev/null`, plugin, needsPlugin),
		"sudo systemctl daemon-reload",
	}
	for _, step := range steps {
		if output, err := testVM.Run(step); err != nil {
			return fmt.Errorf("%s: %w: %s", step, err, output)
		}
	}
	if err := testVM.CopyFile(rpmPath, "/tmp/plugin.rpm"); err != nil {
		return err
	}
	return testVM.CopyFile(oldRPMPath, "/tmp/plugin-1.1.4.rpm")
}

// startPlugin points the installed package at the test pool and starts the plugin, and a unit that
// requires it, as the package's own instructions say.
func startPlugin(t *testing.T) {
	t.Helper()
	run(t, `sudo sed -i 's/^pool = .*/pool = "test_pool"/' /etc/containers/plugin-volume-stratis.conf`)
	run(t, "sudo systemctl enable --now "+plugin+" && sudo systemctl start "+needsPlugin)
	require.NoError(t, waitUntil(30*time.Second, func() bool {
		return activeState(plugin) == "active" && activeState(needsPlugin) == "active"
	}), "the plugin and the unit that requires it should start")
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

func run(t *testing.T, cmd string) {
	t.Helper()
	output, err := testVM.Run(cmd)
	require.NoError(t, err, "%s\n%s", cmd, output)
}

const (
	plugin  = "podman-volume-stratis"
	watcher = "stratisd-fix-ordering.path"
	// needsPlugin stands in for a quadlet volume, whose unit requires the plugin's service
	needsPlugin = "needs-plugin"
)

// Install, upgrade and removal, in that order, through rpm itself, so that rpm decides which
// scripts run and with which arguments.
func TestPackageLifecycle(t *testing.T) {
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

	if !t.Run("upgrade", func(t *testing.T) {
		pluginBefore, dependentBefore := invocation(plugin), invocation(needsPlugin)

		// Reinstalling runs the scripts as an upgrade does: the new copy's posttrans, and the old
		// copy's preremove with 1, as a copy remains.
		run(t, "sudo rpm -Uvh --replacepkgs --nodeps /tmp/plugin.rpm")

		// The restart is not waited for, so give it a moment.
		err := waitUntil(60*time.Second, func() bool {
			return activeState(plugin) == "active" && invocation(plugin) != pluginBefore &&
				activeState(needsPlugin) == "active" && invocation(needsPlugin) != dependentBefore
		})
		assert.NoError(t, err, "the plugin and the unit that requires it should both have restarted: "+
			"plugin %s, %s %s", activeState(plugin), needsPlugin, activeState(needsPlugin))
		assert.True(t, isEnabled(plugin), "the plugin should still be enabled")
		assert.True(t, overrideInstalled(), "the stratisd override should still be installed")
		assert.True(t, isEnabled(watcher), "the path watcher should still be enabled")
	}) {
		return
	}

	t.Run("remove", func(t *testing.T) {
		run(t, "sudo rpm -e --nodeps "+plugin)

		assert.NoError(t, waitUntil(30*time.Second, func() bool { return activeState(plugin) == "inactive" }),
			"the plugin should be stopped")
		assert.False(t, isEnabled(plugin), "the plugin should no longer be enabled")
		assert.Equal(t, "inactive", activeState(needsPlugin), "a unit that requires the plugin stops with it")
		assert.False(t, overrideInstalled(), "the stratisd override should be removed")
		assert.False(t, isEnabled(watcher), "the path watcher should no longer be enabled")
	})
}

// The hosts this was written for run 1.1.3, whose scripts are the same as 1.1.4's. Upgrading from
// those still runs the old removal script, which stops and disables the plugin and removes the
// stratisd override. The new version's posttrans runs after it and puts the override back; the
// plugin stays stopped, as the README says.
func TestUpgradeFromAnOldRelease(t *testing.T) {
	run(t, "sudo rpm -i --nodeps /tmp/plugin-1.1.4.rpm")
	t.Cleanup(func() { _, _ = testVM.Run("sudo rpm -e --nodeps " + plugin) })
	startPlugin(t)

	// The test build's version sorts below 1.1.4 (1.1.4~SNAPSHOT), hence --oldpackage. rpm runs the
	// scripts exactly as for any upgrade.
	run(t, "sudo rpm -Uvh --oldpackage --nodeps /tmp/plugin.rpm")

	assert.True(t, overrideInstalled(), "posttrans should have put the stratisd override back")
	assert.True(t, isEnabled(watcher), "posttrans should have enabled the path watcher again")

	// What the README tells people upgrading from 1.1.4 or older to expect, and to undo by hand.
	assert.Equal(t, "inactive", activeState(plugin), "1.1.4's removal script stops the plugin")
	assert.False(t, isEnabled(plugin), "1.1.4's removal script disables the plugin")
	assert.Equal(t, "inactive", activeState(needsPlugin), "units that require the plugin stop with it")
}
