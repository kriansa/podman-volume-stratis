//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/kriansa/podman-volume-stratis/tests/integration/driverclient"
	"github.com/kriansa/podman-volume-stratis/tests/integration/log"
	"github.com/kriansa/podman-volume-stratis/tests/integration/vm"
)

const (
	socketPath         = "/run/podman/plugins/volume-stratis.sock"
	mountBasePath      = "/mnt/volumes"
	stratisPoolName    = "test_pool"
	systemdWaitTimeout = 30 * time.Second
	socketWaitTimeout  = 30 * time.Second
)

var (
	testVM     vm.VM
	testClient driverclient.DriverClient
)

// TestMain sets up a shared VM for all integration tests
func TestMain(m *testing.M) {
	// Handle interrupt signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fatalf("\nInterrupted, shutting down...")
	}()

	// Start VM
	ctx := context.Background()
	var err error
	testVM, err = vm.StartQEMUVM(ctx)
	if err != nil {
		fatalf("Failed to start VM: %v", err)
	}

	setupVM(ctx, testVM)

	// Create driver client
	testClient = driverclient.NewVMSocketDriverClient(testVM, socketPath)

	log.Status("Running tests...")

	// Run tests
	code := m.Run()

	// Cleanup and exit
	testVM.Stop()
	os.Exit(code)
}

// fatalf prints a formatted error message to stderr and exits with code 1.
// Use this in TestMain or setup code where *testing.T is not available.
func fatalf(format string, args ...any) {
	log.Status(format, args...)
	if testVM != nil {
		testVM.Stop()
	}
	os.Exit(1)
}

// waitForSystemdUnit polls until a systemd unit is active or timeout is reached.
func waitForSystemdUnit(v vm.VM, unit string) error {
	deadline := time.Now().Add(systemdWaitTimeout)
	for time.Now().Before(deadline) {
		if output, _ := v.Run(fmt.Sprintf("systemctl is-active %s", unit)); output == "active\n" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s service not active after %v", unit, systemdWaitTimeout)
}

func waitForSocket(v vm.VM, path string) error {
	deadline := time.Now().Add(socketWaitTimeout)
	for time.Now().Before(deadline) {
		if output, _ := v.Run(fmt.Sprintf("sudo test -S %s && echo -n ok", path)); output == "ok" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("plugin socket %s not created within %v", socketPath, socketWaitTimeout)
}

func setupVM(ctx context.Context, v vm.VM) {
	// Copy plugin binary
	binaryPath := os.Getenv("PLUGIN_BINARY")
	if binaryPath == "" {
		binaryPath = "../../build/dist/podman-volume-stratis"
	}

	if _, err := os.Stat(binaryPath); err != nil {
		fatalf("Plugin binary not found at %s. Run 'make build' first.", binaryPath)
	}

	// Wait for SSH
	if err := testVM.WaitForSSH(ctx); err != nil {
		fatalf("Failed waiting for SSH: %v", err)
	}

	// Wait for stratisd to be ready
	log.Status("Waiting for stratisd service...")
	if err := waitForSystemdUnit(v, "stratisd"); err != nil {
		fatalf("Failed to wait for stratisd: %v", err)
	}

	// Create the test pool if it doesn't exist
	log.Status("Creating stratis pool...")
	if output, err := v.Run("sudo stratis pool create test_pool /dev/loop0"); err != nil {
		fatalf("Failed to create pool: %v\n%s", err, output)
	}

	log.Status("Copying plugin binary to VM...")
	tmpBinaryPath := "/tmp/podman-volume-stratis"
	if err := v.CopyFile(binaryPath, tmpBinaryPath); err != nil {
		fatalf("Failed to copy plugin binary: %v\n", err)
	}
	// Move to final location and make executable
	if _, err := v.Run(fmt.Sprintf("sudo /usr/local/bin/move-plugin-binary.sh %s", tmpBinaryPath)); err != nil {
		fatalf("Failed to install plugin binary: %v", err)
	}

	// Start plugin via systemd
	log.Status("Starting plugin service...")
	if output, err := v.Run("sudo systemctl start podman-volume-stratis"); err != nil {
		fatalf("Failed to start plugin service: %v\n%s", err, output)
	}

	// Wait for plugin to be ready
	log.Status("Waiting for plugin service to be ready...")
	if err := waitForSystemdUnit(v, "podman-volume-stratis"); err != nil {
		fatalf("Failed to wait for the plugin: %v", err)
	}
	log.Status("Waiting for plugin socket...")
	if err := waitForSocket(v, socketPath); err != nil {
		fatalf("Failed to wait for the plugin: %v", err)
	}
}
