//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowMutexName = `Local\OrchestratorDesktopWindow`

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procFindWindow    = user32.NewProc("FindWindowW")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
	windowMutexHandle windows.Handle
)

func claimWindow() bool {
	name, err := windows.UTF16PtrFromString(windowMutexName + profileSuffix())
	if err != nil {
		return true
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		focusOpenWindow()
		return false
	}
	windowMutexHandle = handle
	return true
}

func focusOpenWindow() {
	title, err := syscall.UTF16PtrFromString("Orchestrator")
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}
	const swRestore = 9
	_, _, _ = procShowWindow.Call(hwnd, swRestore)
	_, _, _ = procSetForeground.Call(hwnd)
}
