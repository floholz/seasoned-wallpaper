//go:build !windows

package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// InstallSignals wires POSIX signals to daemon lifecycle events:
//
//   - SIGHUP             → d.Reload()
//   - SIGUSR1            → d.Kick()
//   - SIGUSR2            → d.Wake() (manual resume nudge; useful when D-Bus
//     is unavailable)
//   - SIGTERM / SIGINT   → cancels the returned context so Run exits cleanly
//
// The cleanup closure removes the signal handlers and closes the internal
// goroutine; callers should defer it.
func InstallSignals(parent context.Context, d *Daemon) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	ch := make(chan os.Signal, 4)
	signal.Notify(ch,
		syscall.SIGHUP,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				switch sig {
				case syscall.SIGHUP:
					slog.Info("SIGHUP received, reloading config")
					d.Reload()
				case syscall.SIGUSR1:
					slog.Info("SIGUSR1 received, kicking")
					d.Kick()
				case syscall.SIGUSR2:
					slog.Info("SIGUSR2 received, treating as wake")
					d.Wake()
				case syscall.SIGINT, syscall.SIGTERM:
					slog.Info("shutdown signal received", "signal", sig.String())
					cancel()
					return
				}
			}
		}
	}()

	cleanup := func() {
		signal.Stop(ch)
		cancel()
		<-done
	}
	return ctx, cleanup
}
