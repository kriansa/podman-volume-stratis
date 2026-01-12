//go:build integration

package driverclient

type DriverClient interface {
	Create(name string, opts map[string]string) error
	Remove(name string) error
	Mount(name, id string) (string, error)
	Unmount(name, id string) error
	Path(name string) (string, error)
	Get(name string) (*Volume, error)
	List() ([]*Volume, error)
	Capabilities() (*Capability, error)
}

// Request/Response types matching Docker Volume Plugin protocol

// CreateRequest is the request for VolumeDriver.Create
type CreateRequest struct {
	Name string            `json:"Name"`
	Opts map[string]string `json:"Opts"`
}

// RemoveRequest is the request for VolumeDriver.Remove
type RemoveRequest struct {
	Name string `json:"Name"`
}

// MountRequest is the request for VolumeDriver.Mount
type MountRequest struct {
	Name string `json:"Name"`
	ID   string `json:"ID"`
}

// UnmountRequest is the request for VolumeDriver.Unmount
type UnmountRequest struct {
	Name string `json:"Name"`
	ID   string `json:"ID"`
}

// PathRequest is the request for VolumeDriver.Path
type PathRequest struct {
	Name string `json:"Name"`
}

// GetRequest is the request for VolumeDriver.Get
type GetRequest struct {
	Name string `json:"Name"`
}

// ErrorResponse is a generic error response
type ErrorResponse struct {
	Err string `json:"Err"`
}

// MountResponse is the response from VolumeDriver.Mount
type MountResponse struct {
	Mountpoint string `json:"Mountpoint"`
	Err        string `json:"Err"`
}

// PathResponse is the response from VolumeDriver.Path
type PathResponse struct {
	Mountpoint string `json:"Mountpoint"`
	Err        string `json:"Err"`
}

// Volume represents a volume in list/get responses
type Volume struct {
	Name       string         `json:"Name"`
	Mountpoint string         `json:"Mountpoint"`
	Status     map[string]any `json:"Status"`
}

// GetResponse is the response from VolumeDriver.Get
type GetResponse struct {
	Volume *Volume `json:"Volume"`
	Err    string  `json:"Err"`
}

// ListResponse is the response from VolumeDriver.List
type ListResponse struct {
	Volumes []*Volume `json:"Volumes"`
	Err     string    `json:"Err"`
}

// Capability represents driver capabilities
type Capability struct {
	Scope string `json:"Scope"`
}

// CapabilitiesResponse is the response from VolumeDriver.Capabilities
type CapabilitiesResponse struct {
	Capabilities Capability `json:"Capabilities"`
	Err          string     `json:"Err"`
}

