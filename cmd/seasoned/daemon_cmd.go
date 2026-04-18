package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/floholz/seasoned-wallpaper/internal/daemon"
	"github.com/floholz/seasoned-wallpaper/internal/pidfile"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

// cmdDaemon runs `seasoned daemon` or `seasoned daemon --status`.
func cmdDaemon(g *globalFlags, args []string) int {
	fs := flagSet("daemon")
	status := fs.Bool("status", false, "print status of a running daemon and exit")
	if err := fs.Parse(args); err != nil {
		return exitGeneric
	}
	if *status {
		return cmdDaemonStatus()
	}

	cfg, rc := loadConfig(g, true)
	if rc != 0 {
		return rc
	}

	pidPath, err := pidfile.Path()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	lock, err := pidfile.Acquire(pidPath)
	if err != nil {
		if errors.Is(err, pidfile.ErrLocked) {
			fmt.Fprintf(os.Stderr, "seasoned: daemon already running (pidfile: %s)\n", pidPath)
			return exitAlreadyRunning
		}
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	defer lock.Release()

	statePath, err := state.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}

	d, err := daemon.New(daemon.Options{
		Cfg:       cfg,
		CfgPath:   cfg.Source,
		StatePath: statePath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}

	ctx, stop := daemon.InstallSignals(context.Background(), d)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	return exitOK
}

// cmdDaemonStatus prints whether a daemon is running and exits.
func cmdDaemonStatus() int {
	pidPath, err := pidfile.Path()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	pid, err := pidfile.ReadPID(pidPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Println("not running")
			return exitNoDaemon
		}
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	if !processAlive(pid) {
		fmt.Printf("not running (stale pidfile: %s, pid=%d)\n", pidPath, pid)
		return exitNoDaemon
	}
	fmt.Printf("running (pid=%d, pidfile=%s)\n", pid, pidPath)
	return exitOK
}

// cmdReload signals a running daemon to re-read its config.
func cmdReload(_ *globalFlags, args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "seasoned: unexpected arguments")
		return exitGeneric
	}
	return signalDaemon(daemonSignalReload)
}

// cmdKick signals a running daemon to force a re-roll.
func cmdKick(_ *globalFlags, args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "seasoned: unexpected arguments")
		return exitGeneric
	}
	return signalDaemon(daemonSignalKick)
}

type daemonSignal int

const (
	daemonSignalReload daemonSignal = iota
	daemonSignalKick
)

func signalDaemon(kind daemonSignal) int {
	pidPath, err := pidfile.Path()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	pid, err := pidfile.ReadPID(pidPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "seasoned: no daemon running")
			return exitNoDaemon
		}
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	if !processAlive(pid) {
		fmt.Fprintf(os.Stderr, "seasoned: no daemon running (stale pidfile, pid=%d)\n", pid)
		return exitNoDaemon
	}
	return sendDaemonSignal(pid, kind)
}
