//go:build integration

package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/kriansa/podman-volume-stratis/tests/integration/log"
)

// QEMU represents a running QEMU virtual machine
type QEMU struct {
	cmd          *exec.Cmd
	sshClient    *ssh.Client
	config       *QEMUConfig
	snapshotPath string
	mu           sync.Mutex
}

// QEMUConfig holds configuration for starting a VM
type QEMUConfig struct {
	ImagePath  string
	SSHPort    int
	SSHUser    string
	SSHPass    string
	SSHTimeout time.Duration
	Memory     int
	CPUs       int
}

func StartQEMUVM(ctx context.Context) (*QEMU, error) {
	imagePath, err := getImagePath()
	if err != nil {
		return nil, err
	}

	config := QEMUConfig{
		SSHPort:    10022,
		Memory:     2048,
		CPUs:       2,
		SSHUser:    "fedora",
		SSHPass:    "fedora",
		SSHTimeout: 1 * time.Minute,
		ImagePath:  imagePath,
	}

	return StartQEMUVMWithConfig(ctx, config)
}

// snapshotDir picks where the VM's snapshot goes. It holds everything the VM writes, several
// gigabytes by the end of a run. The usual temporary directory is often kept in memory and too
// small for that: once it fills up, the VM's disk fails with I/O errors. So the snapshot goes next
// to the image, unless VM_SNAPSHOT_DIR says where it should go. Not TMPDIR, which tools set for
// their own reasons, often to a directory in that same small memory-backed /tmp.
func snapshotDir(imagePath string) string {
	if dir := os.Getenv("VM_SNAPSHOT_DIR"); dir != "" {
		return dir
	}
	return filepath.Dir(imagePath)
}

// removeAbandonedSnapshots deletes the snapshots of earlier runs that ended without cleaning up,
// such as a killed one. Each is named after the process that made it, so a snapshot whose process
// is gone is no longer in use. Those of runs still going are left alone.
func removeAbandonedSnapshots(dir string) {
	paths, _ := filepath.Glob(filepath.Join(dir, "vm-test-*.qcow2"))
	for _, path := range paths {
		var pid int
		if _, err := fmt.Sscanf(filepath.Base(path), "vm-test-%d.qcow2", &pid); err != nil {
			continue
		}
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			log.Status("Removing the snapshot of an earlier run: %s", path)
			_ = os.Remove(path)
		}
	}
}

// StartVM launches a QEMU VM and waits for SSH to become available
func StartQEMUVMWithConfig(ctx context.Context, config QEMUConfig) (*QEMU, error) {
	if config.ImagePath == "" {
		return nil, fmt.Errorf("image path is required")
	}

	// Verify the image exists
	if _, err := os.Stat(config.ImagePath); err != nil {
		return nil, fmt.Errorf("image not found: %w", err)
	}

	// Create a snapshot to avoid modifying the base image
	snapshotDir := snapshotDir(config.ImagePath)
	removeAbandonedSnapshots(snapshotDir)
	snapshotPath := filepath.Join(snapshotDir, fmt.Sprintf("vm-test-%d.qcow2", os.Getpid()))

	// Create snapshot
	createCmd := exec.CommandContext(ctx, "qemu-img", "create",
		"-f", "qcow2",
		"-b", config.ImagePath,
		"-F", "qcow2",
		snapshotPath,
	)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create snapshot: %w: %s", err, output)
	}

	// Start QEMU
	log.Status("Starting VM with image: %s", config.ImagePath)
	cmd := exec.CommandContext(ctx, "qemu-system-x86_64",
		"-m", fmt.Sprintf("%dM", config.Memory),
		"-smp", fmt.Sprintf("%d", config.CPUs),
		"-machine", "type=pc,accel=kvm",
		"-cpu", "host",
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback,discard=ignore,format=qcow2", snapshotPath),
		"-boot", "c",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", config.SSHPort),
		"-device", "virtio-net,netdev=net0",
		"-nographic",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// QEMU is killed along with the test process, however that ends: a -test.timeout panic or a
	// SIGKILL runs no cleanup, and a VM left running keeps its SSH port, so the next run's SSH
	// would reach the old VM instead of its own.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start qemu: %w", err)
	}

	vm := &QEMU{
		cmd:          cmd,
		config:       &config,
		snapshotPath: snapshotPath,
	}

	return vm, nil
}

func getImagePath() (string, error) {
	imagePath := os.Getenv("VM_IMAGE")
	if imagePath == "" {
		imagePath = "../images/fedora-stratis.qcow2"
	}

	if _, err := os.Stat(imagePath); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("VM image not found. Run 'make test-image' first or set VM_IMAGE env var")
	}

	absImagePath, err := filepath.Abs(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	return absImagePath, nil
}

// WaitForSSH polls until SSH is available
func (vm *QEMU) WaitForSSH(ctx context.Context) error {
	config := &ssh.ClientConfig{
		User:            vm.config.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.Password(vm.config.SSHPass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	deadline := time.Now().Add(vm.config.SSHTimeout)
	var lastErr error

	log.Status("Waiting for SSH to become available...")
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := ssh.Dial("tcp", fmt.Sprintf("localhost:%d", vm.config.SSHPort), config)
		if err == nil {
			vm.sshClient = conn
			return nil
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("ssh timeout after %v: %w", vm.config.SSHTimeout, lastErr)
}

// Run executes a command in the VM via SSH
func (vm *QEMU) Run(cmd string) (string, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.sshClient == nil {
		return "", fmt.Errorf("ssh client not connected")
	}

	session, err := vm.sshClient.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	output, err := session.CombinedOutput(cmd)
	return string(output), err
}

// RunWithTimeout executes a command with a specific timeout
func (vm *QEMU) RunWithTimeout(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		output string
		err    error
	}

	ch := make(chan result, 1)
	go func() {
		output, err := vm.Run(cmd)
		ch <- result{output, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.output, r.err
	}
}

// CopyFile copies a local file to the VM using SFTP
func (vm *QEMU) CopyFile(localPath, remotePath string) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.sshClient == nil {
		return fmt.Errorf("ssh client not connected")
	}

	// Read local file
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Create SFTP client
	sftpClient, err := sftp.NewClient(vm.sshClient)
	if err != nil {
		return fmt.Errorf("create sftp client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	// Create parent directories
	dir := filepath.Dir(remotePath)
	if err := sftpClient.MkdirAll(dir); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Create and write file
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Set permissions
	if err := sftpClient.Chmod(remotePath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	return nil
}

// Gracefully shuts down the VM
func (vm *QEMU) Stop() {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.sshClient != nil {
		// Try graceful shutdown first
		session, err := vm.sshClient.NewSession()
		if err == nil {
			_ = session.Run("sudo shutdown -P now")
			_ = session.Close()
			time.Sleep(2 * time.Second)
		}
		_ = vm.sshClient.Close()
		vm.sshClient = nil
	}

	log.Status("Shutting down VM...")
	if vm.cmd != nil && vm.cmd.Process != nil {
		_ = vm.cmd.Process.Kill()
		_ = vm.cmd.Wait()
		vm.cmd = nil
	}

	// Clean up snapshot
	if vm.snapshotPath != "" {
		log.Status("Cleaning up disk snapshot...")
		_ = os.Remove(vm.snapshotPath)
		vm.snapshotPath = ""
	}
}

// IsRunning checks if the VM process is still running
func (vm *QEMU) IsRunning() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.cmd == nil || vm.cmd.Process == nil {
		return false
	}

	// Check if process is still running
	return vm.cmd.ProcessState == nil
}
