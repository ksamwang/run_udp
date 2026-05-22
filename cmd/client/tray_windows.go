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
		systray.SetTooltip("UDP Tunnel 客户端：" + deviceID)
		label := "设备：" + deviceID
		if actions.RuntimeStatus != nil {
			label += " [" + actions.RuntimeStatus() + "]"
		}
		status := systray.AddMenuItem(label, "当前设备")
		status.Disable()
		version := systray.AddMenuItem("版本："+Version, "当前客户端版本")
		version.Disable()
		open := systray.AddMenuItem("打开管理后台", "打开 Web 管理后台")
		config := systray.AddMenuItem("客户端配置", "打开本机引导配置页")
		logs := systray.AddMenuItem("打开日志目录", "打开客户端日志目录")
		restart := systray.AddMenuItem("重启服务", "重启 Windows 服务")
		updates := systray.AddMenuItem("检查更新", "立即检查客户端更新")
		systray.AddSeparator()
		exit := systray.AddMenuItem("退出托盘", "退出托盘助手")
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
