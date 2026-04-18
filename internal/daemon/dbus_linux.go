//go:build linux

package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/godbus/dbus/v5"
)

// watchSleepWake subscribes to org.freedesktop.login1's PrepareForSleep
// signal. A payload of `false` means the system has resumed — we nudge
// d.Wake() so the loop re-evaluates immediately instead of waiting for
// the next scheduled tick. A payload of `true` (about to sleep) is
// ignored; there's nothing to do.
//
// Runs until ctx is cancelled. Returns an error on setup failure; a
// subsequent signal-channel close is treated as a clean shutdown.
func (d *Daemon) watchSleepWake(ctx context.Context) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("dbus: connect system bus: %w", err)
	}
	defer conn.Close()

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		return fmt.Errorf("dbus: add match: %w", err)
	}

	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	slog.Debug("dbus sleep/wake watch started")

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-ch:
			if !ok {
				return nil
			}
			if sig == nil || sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" {
				continue
			}
			if len(sig.Body) < 1 {
				continue
			}
			aboutToSleep, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}
			if aboutToSleep {
				slog.Debug("dbus: PrepareForSleep(true) — suspending")
				continue
			}
			slog.Info("dbus: PrepareForSleep(false) — resumed")
			d.Wake()
		}
	}
}
