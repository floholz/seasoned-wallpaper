// Package setter applies a wallpaper via platform-specific backends.
// Platform detection lives in the build-tagged *_linux/_windows/_darwin.go files.
package setter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Setter applies a wallpaper path and describes itself for diagnostics.
type Setter interface {
	Apply(ctx context.Context, path string) error
	Describe() string
}

// Options configure backend selection.
type Options struct {
	// LinuxCommand is an optional shell-command template; {{.Path}} is
	// substituted with the shell-quoted absolute wallpaper path. When set,
	// Linux detection is bypassed.
	LinuxCommand string

	// Runner is the command runner used by backends. Injected for tests;
	// leave nil to use the OS default.
	Runner CommandRunner
}

// CommandRunner is the backend's view of the host shell. All exec, binary
// lookup, and env access goes through this interface so tests can mock it.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(name string) (string, bool)
	Getenv(key string) string
}

// New returns a Setter suitable for the current GOOS.
func New(opts Options) (Setter, error) {
	if opts.Runner == nil {
		opts.Runner = OSRunner{}
	}
	return newPlatform(opts)
}

// OSRunner is the default CommandRunner backed by os/exec and os.Getenv.
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return nil
}

func (OSRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (OSRunner) LookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

func (OSRunner) Getenv(key string) string { return os.Getenv(key) }

// cmdSetter is a thin adapter used by most backends.
type cmdSetter struct {
	name  string
	apply func(ctx context.Context, path string) error
}

func (c *cmdSetter) Apply(ctx context.Context, path string) error { return c.apply(ctx, path) }
func (c *cmdSetter) Describe() string                             { return c.name }
