//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole connects a GUI-subsystem binary to the console it was started
// from, so CLI commands print output when run directly from cmd/PowerShell.
// Redirected standard handles (pipes, files) are left untouched.
func attachConsole() {
	// Output redirected to a pipe or file (CI, `> out.txt`, a parent process
	// capturing output): keep the inherited handles.
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != windows.InvalidHandle && h != 0 {
		if t, err := windows.GetFileType(h); err == nil && (t == windows.FILE_TYPE_PIPE || t == windows.FILE_TYPE_DISK) {
			return
		}
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
