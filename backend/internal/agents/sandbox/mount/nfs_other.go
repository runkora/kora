//go:build !linux

package mount

import (
	"context"
	"errors"
)

// errUnsupported reports a platform without the kernel NFS server and client.
var errUnsupported = errors.New("sandbox folder mounts need Linux")

// Server is unavailable off Linux.
type Server struct{}

// StartServer fails off Linux.
func StartServer(context.Context, ServerConfig) (*Server, <-chan error, error) {
	return nil, nil, errUnsupported
}

// Apply fails off Linux.
func (*Server) Apply(context.Context, []export) error { return errUnsupported }

// Mounts fails off Linux.
func (Client) Mounts() (map[string]string, error) { return nil, errUnsupported }

// Resolve fails off Linux.
func (Client) Resolve(string, string) (string, error) { return "", errUnsupported }

// Mount fails off Linux.
func (Client) Mount(string, string, string) error { return errUnsupported }

// Release fails off Linux.
func (Client) Release([]string, map[string]bool) error { return errUnsupported }

// Unmount fails off Linux.
func (Client) Unmount(string) error { return errUnsupported }
