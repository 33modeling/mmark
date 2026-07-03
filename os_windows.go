//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procMessageBoxW   = user32.NewProc("MessageBoxW")
)

// attachConsole reattaches stdout/stderr to the parent console. The release
// binary is built with -H windowsgui (no console window on double-click),
// which otherwise leaves --version and error output going nowhere when the
// program is run from cmd/PowerShell.
func attachConsole() {
	const attachParentProcess = ^uintptr(0) & 0xFFFFFFFF
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
}

// fatalUI shows startup errors in a message box — with -H windowsgui and no
// parent console (double-click launch) there is nowhere else they could go.
func fatalUI(msg string) {
	const mbIconError = 0x10
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("mmark")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
}
