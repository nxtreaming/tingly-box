// Package process provides the seam between agentboot and the OS process that
// implements an agent (e.g. the `claude` CLI binary).
//
// Production code uses [OSExecFactory], which wraps os/exec. Tests use
// [FakeFactory] to substitute the binary with a scripted in-memory process,
// keeping the rest of the agent stack (driver, decoder, runner) on the real
// code path.
package process

import (
	"context"
	"io"
)

// LaunchSpec describes how to start an agent process.
//
// It intentionally mirrors the subset of os/exec.Cmd that an agent driver
// needs to populate, so that drivers can be unit-tested against any Factory.
type LaunchSpec struct {
	Path    string
	Args    []string
	Env     []string
	WorkDir string
}

// Handle is a running process with attached stdin/stdout pipes.
//
// Lifecycle invariants:
//   - Stdin and Stdout are valid until Wait returns.
//   - Done is closed after the process has exited; Wait then returns
//     immediately with the exit error.
//   - Kill is idempotent and safe to call concurrently with Wait.
type Handle interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Wait() error
	Kill() error
	Done() <-chan struct{}
}

// Factory starts agent processes.
type Factory interface {
	Start(ctx context.Context, spec LaunchSpec) (Handle, error)
}
