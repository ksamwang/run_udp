//go:build windows

package main

import (
	"log"
	"os/exec"

	"github.com/getlantern/systray"
)

func runTray(deviceID, controlURL, configURL string, quit func()) {
	systray.Run(func() {
		systray.SetIcon(trayIconICO())
		systray.SetTitle("UDP Tunnel")
		systray.SetTooltip("UDP Tunnel agent: " + deviceID)
		status := systray.AddMenuItem("Device: "+deviceID, "Current device")
		status.Disable()
		open := systray.AddMenuItem("Open Control Plane", "Open web management")
		config := systray.AddMenuItem("Client Settings", "Open local client settings")
		systray.AddSeparator()
		exit := systray.AddMenuItem("Exit", "Exit UDP Tunnel")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", controlURL).Start(); err != nil {
						log.Printf("open browser failed: %v", err)
					}
				case <-config.ClickedCh:
					if configURL == "" {
						continue
					}
					if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", configURL).Start(); err != nil {
						log.Printf("open client settings failed: %v", err)
					}
				case <-exit.ClickedCh:
					systray.Quit()
					quit()
					return
				}
			}
		}()
	}, func() {})
}
