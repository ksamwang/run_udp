//go:build windows

package main

import (
	"log"

	"udp_tunnel_demo/internal/lan"

	"github.com/getlantern/systray"
)

type trayActions struct {
	OpenConfig func()
	OpenLogs   func() error
	Restart    func() error
}

func runTray(deviceID, controlURL, configURL string, actions trayActions, quit func()) {
	systray.Run(func() {
		systray.SetTitle(lan.TrayName)
		systray.SetTooltip(lan.TrayName + "：" + deviceID)
		status := systray.AddMenuItem("设备："+deviceID, "当前 LAN 设备")
		status.Disable()
		version := systray.AddMenuItem("版本："+Version, "当前 LAN 客户端版本")
		version.Disable()
		config := systray.AddMenuItem("LAN 配置", "打开本机 LAN 配置页")
		logs := systray.AddMenuItem("打开日志目录", "打开 LAN 日志目录")
		restart := systray.AddMenuItem("重启 LAN 服务", "重启 Windows 服务")
		systray.AddSeparator()
		exit := systray.AddMenuItem("退出托盘", "退出托盘助手")
		go func() {
			for {
				select {
				case <-config.ClickedCh:
					if actions.OpenConfig != nil {
						actions.OpenConfig()
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
				case <-exit.ClickedCh:
					systray.Quit()
					quit()
					return
				}
			}
		}()
	}, func() {})
}
