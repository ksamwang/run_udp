//go:build !windows

package main

import "errors"

func launchUpdater(pkgPath string) error    { return errors.New("windows only") }
func runUpdaterHelper(pkgPath string) error { return errors.New("windows only") }
