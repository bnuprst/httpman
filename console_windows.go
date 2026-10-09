//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole connects a GUI-subsystem binary to the console it was started
// from, so CLI commands print output when run directly from cmd/PowerShell.
// When standard handles were inherited (pipes, redirects, the npm launcher)
// they are left untouched.
func attachConsole() {
	if validHandle(windows.STD_OUTPUT_HANDLE) {
		return
	}
	const attachParentProcess = ^uint32(0) // ATTACH_PARENT_PROCESS (-1)
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := kernel32.NewProc("AttachConsole").Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
}

func validHandle(std uint32) bool {
	h, err := windows.GetStdHandle(std)
	if err != nil || h == windows.InvalidHandle || h == 0 {
		return false
	}
	t, err := windows.GetFileType(h)
	return err == nil && t != windows.FILE_TYPE_UNKNOWN
}
