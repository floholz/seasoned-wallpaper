//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"github.com/floholz/seasoned-wallpaper/internal/daemon"
)

// processAlive probes whether pid is a live process. OpenProcess with
// PROCESS_QUERY_LIMITED_INFORMATION fails for dead/unknown pids.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exit uint32
	if err := windows.GetExitCodeProcess(h, &exit); err != nil {
		return false
	}
	return exit == 259 // STILL_ACTIVE
}

// sendDaemonSignal on Windows uses the sentinel directory; POSIX signals
// are not available.
func sendDaemonSignal(_ int, kind daemonSignal) int {
	name := "reload"
	if kind == daemonSignalKick {
		name = "kick"
	}
	dir, err := daemon.ControlDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	if err := daemon.TouchControl(dir, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	return exitOK
}
