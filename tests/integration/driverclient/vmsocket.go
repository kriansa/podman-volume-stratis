//go:build integration

package driverclient

import (
	"encoding/json"
	"fmt"

	"github.com/kriansa/podman-volume-stratis/tests/integration/vm"
)

// VMSocketClient provides methods for calling the Docker volume plugin API
type VMSocketClient struct {
	vm         vm.VM
	socketPath string
}

// NewVMSocketDriverClient creates a new client for interacting with the volume driver
func NewVMSocketDriverClient(vm vm.VM, socketPath string) *VMSocketClient {
	return &VMSocketClient{
		vm:         vm,
		socketPath: socketPath,
	}
}

// callDriver makes an HTTP request to the volume driver over the Unix socket
// Since we can't directly access the socket from the host, we use curl inside the VM
func (c *VMSocketClient) callDriver(method string, request, response any) error {
	reqBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Use curl to call the Unix socket from inside the VM
	cmd := fmt.Sprintf(
		`sudo curl -s --unix-socket %s -X POST -H "Content-Type: application/json" -d '%s' http://localhost/%s`,
		c.socketPath,
		string(reqBody),
		method,
	)

	output, err := c.vm.Run(cmd)
	if err != nil {
		return fmt.Errorf("call driver: %w: %s", err, output)
	}

	if err := json.Unmarshal([]byte(output), response); err != nil {
		return fmt.Errorf("unmarshal response: %w: %s", err, output)
	}

	return nil
}

// Create creates a new volume
func (c *VMSocketClient) Create(name string, opts map[string]string) error {
	req := CreateRequest{Name: name, Opts: opts}
	var resp ErrorResponse
	if err := c.callDriver("VolumeDriver.Create", req, &resp); err != nil {
		return err
	}
	if resp.Err != "" {
		return fmt.Errorf("driver error: %s", resp.Err)
	}
	return nil
}

// Remove removes a volume
func (c *VMSocketClient) Remove(name string) error {
	req := RemoveRequest{Name: name}
	var resp ErrorResponse
	if err := c.callDriver("VolumeDriver.Remove", req, &resp); err != nil {
		return err
	}
	if resp.Err != "" {
		return fmt.Errorf("driver error: %s", resp.Err)
	}
	return nil
}

// Mount mounts a volume and returns the mount path
func (c *VMSocketClient) Mount(name, id string) (string, error) {
	req := MountRequest{Name: name, ID: id}
	var resp MountResponse
	if err := c.callDriver("VolumeDriver.Mount", req, &resp); err != nil {
		return "", err
	}
	if resp.Err != "" {
		return "", fmt.Errorf("driver error: %s", resp.Err)
	}
	return resp.Mountpoint, nil
}

// Unmount unmounts a volume
func (c *VMSocketClient) Unmount(name, id string) error {
	req := UnmountRequest{Name: name, ID: id}
	var resp ErrorResponse
	if err := c.callDriver("VolumeDriver.Unmount", req, &resp); err != nil {
		return err
	}
	if resp.Err != "" {
		return fmt.Errorf("driver error: %s", resp.Err)
	}
	return nil
}

// Path returns the mount path for a volume
func (c *VMSocketClient) Path(name string) (string, error) {
	req := PathRequest{Name: name}
	var resp PathResponse
	if err := c.callDriver("VolumeDriver.Path", req, &resp); err != nil {
		return "", err
	}
	if resp.Err != "" {
		return "", fmt.Errorf("driver error: %s", resp.Err)
	}
	return resp.Mountpoint, nil
}

// Get returns information about a volume
func (c *VMSocketClient) Get(name string) (*Volume, error) {
	req := GetRequest{Name: name}
	var resp GetResponse
	if err := c.callDriver("VolumeDriver.Get", req, &resp); err != nil {
		return nil, err
	}
	if resp.Err != "" {
		return nil, fmt.Errorf("driver error: %s", resp.Err)
	}
	return resp.Volume, nil
}

// List returns all volumes
func (c *VMSocketClient) List() ([]*Volume, error) {
	var resp ListResponse
	if err := c.callDriver("VolumeDriver.List", struct{}{}, &resp); err != nil {
		return nil, err
	}
	if resp.Err != "" {
		return nil, fmt.Errorf("driver error: %s", resp.Err)
	}
	return resp.Volumes, nil
}

// Capabilities returns the driver capabilities
func (c *VMSocketClient) Capabilities() (*Capability, error) {
	var resp CapabilitiesResponse
	if err := c.callDriver("VolumeDriver.Capabilities", struct{}{}, &resp); err != nil {
		return nil, err
	}
	if resp.Err != "" {
		return nil, fmt.Errorf("driver error: %s", resp.Err)
	}
	return &resp.Capabilities, nil
}
