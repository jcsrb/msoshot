//go:build windows

// Package winconsole lets a GUI-subsystem exe print to the console it was
// started from, for the --check, --render-file and --headless modes.
package winconsole

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// usable reports whether a standard handle already points somewhere real,
// e.g. "outlook-render --check > out.txt" or a pipe. Those are kept as they are.
func usable(std uint32) bool {
	h, err := windows.GetStdHandle(std)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	t, err := windows.GetFileType(h)
	return err == nil && t != windows.FILE_TYPE_UNKNOWN
}

// Attach connects stdout/stderr to the parent console when they aren't
// already redirected somewhere.
func Attach() {
	outOK, errOK := usable(windows.STD_OUTPUT_HANDLE), usable(windows.STD_ERROR_HANDLE)
	if outOK && errOK {
		return
	}
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	if !outOK {
		os.Stdout = f
	}
	if !errOK {
		os.Stderr = f
	}
}
