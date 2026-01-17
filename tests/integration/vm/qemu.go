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
	snapshotPath := filepath.Join(os.TempDir(), fmt.Sprintf("vm-test-%d.qcow2", os.Getpid()))

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
