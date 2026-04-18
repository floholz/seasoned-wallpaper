//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// processAlive reports whether pid names a running process. kill(0) is
// the portable probe: it does no work but fails with ESRCH if the pid
// is dead. EPERM ("alive but not ours") counts as alive.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

func sendDaemonSignal(pid int, kind daemonSignal) int {
	sig := syscall.SIGHUP
	if kind == daemonSignalKick {
		sig = syscall.SIGUSR1
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	if err := proc.Signal(sig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	return exitOK
}
