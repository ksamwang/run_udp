//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func launchUpdater(pkgPath string) error {
	exe := currentExePath()
	if exe == "" {
		return errors.New("executable path unavailable")
	}
	cmd := exec.Command(exe, "-updater", "-update-package", pkgPath)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func runUpdaterHelper(pkgPath string) error {
	if pkgPath == "" {
		return errors.New("missing update package")
	}
	_ = stopWindowsService()
	for i := 0; i < 60; i++ {
		st, _ := queryWindowsServiceStatus()
		if st == "stopped" || st == "missing" || st == "unknown" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	cmd := exec.Command(pkgPath, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
	if err := cmd.Run(); err != nil {
		_ = startWindowsService()
		return fmt.Errorf("run installer: %w", err)
	}
	return startWindowsService()
}
