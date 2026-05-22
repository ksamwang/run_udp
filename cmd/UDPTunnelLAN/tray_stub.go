//go:build !windows

package main

import "log"

type trayActions struct{}

func runTray(deviceID, controlURL, configURL string, actions trayActions, quit func()) {
	log.Printf("[%s] LAN tray is only available on Windows", deviceID)
}
