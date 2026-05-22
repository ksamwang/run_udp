//go:build windows

package main

import "golang.org/x/sys/windows"

var procReleaseMutex = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReleaseMutex")

func acquireTraySingleInstance() (func(), bool, error) {
	name, err := windows.UTF16PtrFromString(`Local\UDPTunnelTray`)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, false, err
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(handle)
		return nil, false, nil
	}
	release := func() {
		_, _, _ = procReleaseMutex.Call(uintptr(handle))
		_ = windows.CloseHandle(handle)
	}
	return release, true, nil
}
