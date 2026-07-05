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
	comdlg32          = syscall.NewLazyDLL("comdlg32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procMessageBoxW   = user32.NewProc("MessageBoxW")
	procGetOpenFile   = comdlg32.NewProc("GetOpenFileNameW")
)

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

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

func canChooseMarkdownFile() bool { return true }

func chooseMarkdownFile() (string, bool) {
	const (
		ofnExplorer      = 0x00080000
		ofnFileMustExist = 0x00001000
		ofnHideReadOnly  = 0x00000004
		ofnPathMustExist = 0x00000800
	)
	file := make([]uint16, 32768)
	filter := syscall.StringToUTF16("Markdown files (*.md;*.markdown;*.mdown;*.mkd)\x00*.md;*.markdown;*.mdown;*.mkd\x00All files (*.*)\x00*.*\x00")
	title := syscall.StringToUTF16("마크다운 파일 열기")
	defExt := syscall.StringToUTF16("md")
	ofn := openFileName{
		lStructSize: uint32(unsafe.Sizeof(openFileName{})),
		lpstrFilter: &filter[0],
		lpstrFile:   &file[0],
		nMaxFile:    uint32(len(file)),
		lpstrTitle:  &title[0],
		flags:       ofnExplorer | ofnFileMustExist | ofnHideReadOnly | ofnPathMustExist,
		lpstrDefExt: &defExt[0],
	}
	r, _, _ := procGetOpenFile.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(file), true
}
