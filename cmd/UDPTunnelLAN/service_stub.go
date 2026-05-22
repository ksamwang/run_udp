//go:build !windows

package main

import (
	"context"
	"errors"
)

func runWindowsService(run func(context.Context)) error {
	run(context.Background())
	return nil
}
func installWindowsService(exePath, configPath string) error { return errors.New("windows only") }
func uninstallWindowsService() error                         { return errors.New("windows only") }
func startWindowsService() error                             { return errors.New("windows only") }
func stopWindowsService() error                              { return errors.New("windows only") }
func restartWindowsService() error                           { return errors.New("windows only") }
func queryWindowsServiceStatus() (string, error)             { return "unsupported", nil }
func ensureTrayStartup(exePath, configPath string) error     { return nil }
func removeTrayStartup() error                               { return nil }
func openLogs() error                                        { return errors.New("windows only") }
func spawnServiceCommand(arg string) error                   { return errors.New("windows only") }
