//go:build !windows

package main

import "log"

func runTray(deviceID, controlURL, configURL string, quit func()) {
	log.Printf("[%s] tray is only available on Windows (control=%s config=%s)", deviceID, controlURL, configURL)
}
