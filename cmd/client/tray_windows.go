//go:build windows

package main

import (
	"log"
	"os/exec"

	"github.com/getlantern/systray"
)

type trayActions struct {
	OpenLogs      func() error
	Restart       func() error
	CheckUpdates  func() error
	RuntimeStatus func() string
}

func runTray(deviceID, controlURL, configURL string, actions trayActions, quit func()) {
	systray.Run(func() {
		systray.SetIcon(trayIconICO())
		systray.SetTitle("UDP Tunnel")
		systray.SetTooltip("UDP Tunnel agent: " + deviceID)
		label := "Device: " + deviceID
		if actions.RuntimeStatus != nil {
			label += " [" + actions.RuntimeStatus() + "]"
		}
		status := systray.AddMenuItem(label, "Current device")
		status.Disable()
		version := systray.AddMenuItem("Version: "+Version, "Current client version")
		version.Disable()
		open := systray.AddMenuItem("Open Control Plane", "Open web management")
		config := systray.AddMenuItem("Client Settings", "Open local client settings")
		logs := systray.AddMenuItem("Open Logs", "Open log directory")
		restart := systray.AddMenuItem("Restart Service", "Restart Windows service")
		updates := systray.AddMenuItem("Check for Updates", "Check for updates now")
		systray.AddSeparator()
		exit := systray.AddMenuItem("Exit Tray", "Exit tray helper")
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
				case <-logs.ClickedCh:
					if actions.OpenLogs != nil {
						if err := actions.OpenLogs(); err != nil {
							log.Printf("open logs failed: %v", err)
						}
					}
				case <-restart.ClickedCh:
					if actions.Restart != nil {
						if err := actions.Restart(); err != nil {
							log.Printf("restart service failed: %v", err)
						}
					}
				case <-updates.ClickedCh:
					if actions.CheckUpdates != nil {
						if err := actions.CheckUpdates(); err != nil {
							log.Printf("check updates failed: %v", err)
						}
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
