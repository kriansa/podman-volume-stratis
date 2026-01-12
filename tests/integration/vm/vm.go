//go:build integration

package vm

import (
	"context"
	"time"
)

type VM interface {
	Run(cmd string) (string, error)
	RunWithTimeout(ctx context.Context, cmd string, timeout time.Duration) (string, error)
	CopyFile(localPath, remotePath string) error
	Stop()
	IsRunning() bool
	WaitForSSH(ctx context.Context) error
}
