//go:build !windows

package main

import "log"

func openBrowser(target string) {
	if target == "" {
		return
	}
	log.Printf("open browser is only implemented on Windows; open manually: %s", target)
}
