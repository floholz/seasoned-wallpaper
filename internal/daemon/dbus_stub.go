//go:build !linux

package daemon

import "context"

// watchSleepWake is a no-op on non-Linux platforms. macOS would need
// IOPMrootDomain (CGo) and Windows has no equivalent userspace signal
// worth chasing for v2. The drift-detection fallback in the main loop
// handles wake-from-suspend well enough on those systems.
func (d *Daemon) watchSleepWake(_ context.Context) error { return nil }
