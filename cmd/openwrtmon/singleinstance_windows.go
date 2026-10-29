//go:build windows

package main

import (
	"unsafe"
	"golang.org/x/sys/windows"
)

var (
	k32          = windows.NewLazySystemDLL("kernel32.dll")
	pCreateMutex = k32.NewProc("CreateMutexW")
)

func acquireSingleInstance() (windows.Handle, bool) {
	namePtr, err := windows.UTF16PtrFromString("Local\\OpenWRTMonitorSingleInstanceMutex")
	if err != nil {
		return 0, true
	}
	r1, _, errCall := pCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if r1 == 0 {
		return 0, true
	}
	if errno, ok := errCall.(windows.Errno); ok && errno == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(windows.Handle(r1))
		return 0, false
	}
	return windows.Handle(r1), true
}

