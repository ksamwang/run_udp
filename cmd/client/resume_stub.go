//go:build !windows

package main

import "context"

func startResumeMonitor(ctx context.Context) <-chan string {
	ch := make(chan string)
	go func() {
		<-ctx.Done()
	}()
	return ch
}
