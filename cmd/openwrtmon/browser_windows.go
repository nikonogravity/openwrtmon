//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func openBrowser(url string) {
	verbPtr, _ := windows.UTF16PtrFromString("open")
	urlPtr, _ := windows.UTF16PtrFromString(url)
	s32 := windows.NewLazySystemDLL("shell32.dll")
	pShellExecute := s32.NewProc("ShellExecuteW")
	pShellExecute.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(urlPtr)), 0, 0, 1)
}
