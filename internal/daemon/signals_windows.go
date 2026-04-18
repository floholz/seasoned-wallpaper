//go:build windows

package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// InstallSignals wires the subset of signals Go exposes on Windows to
// daemon lifecycle events. Only shutdown is meaningful — reload and kick
// travel through the sentinel directory instead (see watchSentinel).
//
//	SIGINT / SIGTERM / SIGBREAK → cancels the returned context
func InstallSignals(parent context.Context, _ *Daemon) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)

	ch := make(chan os.Signal, 2)
	signal.Notify(ch,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-ch:
			if !ok {
				return
			}
			slog.Info("shutdown signal received", "signal", sig.String())
			cancel()
		}
	}()

	cleanup := func() {
		signal.Stop(ch)
		cancel()
		<-done
	}
	return ctx, cleanup
}
