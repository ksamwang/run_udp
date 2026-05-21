//go:build windows

package main

import (
	"log"
	"os/exec"
)

func openBrowser(target string) {
	if target == "" {
		return
	}
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start(); err != nil {
		log.Printf("open browser failed: %v", err)
	}
}
