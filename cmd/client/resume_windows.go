//go:build windows

package main

import (
	"context"
	"time"
)

// startResumeMonitor 用最小代价感知 Windows 上的休眠恢复。
// 这里不引入复杂的窗口消息监听，而是通过墙钟跳变检测系统长时间挂起后恢复。
func startResumeMonitor(ctx context.Context) <-chan string {
	ch := make(chan string, 1)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		last := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if now.Sub(last) > 45*time.Second {
					select {
					case ch <- "system_resume":
					default:
					}
				}
				last = now
			}
		}
	}()
	return ch
}
