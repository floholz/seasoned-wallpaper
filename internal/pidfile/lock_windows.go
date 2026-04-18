//go:build windows

package pidfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockExclusive(f *os.File) error {
	handle := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1, 0,
		ol,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
			errors.Is(err, windows.ERROR_IO_PENDING) ||
			errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return ErrLocked
		}
		return err
	}
	return nil
}

func unlock(f *os.File) error {
	handle := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(handle, 0, 1, 0, ol)
}
