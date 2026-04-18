//go:build windows

package setter

import (
	"context"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SystemParametersInfoW constants.
const (
	spiSetDeskWallpaper = 0x0014
	spifUpdateIniFile   = 0x01
	spifSendChange      = 0x02
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
)

func newPlatform(_ Options) (Setter, error) {
	return &cmdSetter{
		name:  "windows/SystemParametersInfoW",
		apply: applyWindows,
	}, nil
}

func applyWindows(_ context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("windows: resolve abs path: %w", err)
	}
	p, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return fmt.Errorf("windows: encode path: %w", err)
	}
	ret, _, callErr := procSystemParametersInfoW.Call(
		uintptr(spiSetDeskWallpaper),
		0,
		uintptr(unsafe.Pointer(p)),
		uintptr(spifUpdateIniFile|spifSendChange),
	)
	if ret == 0 {
		return fmt.Errorf("windows: SystemParametersInfoW failed: %v", callErr)
	}
	return nil
}
