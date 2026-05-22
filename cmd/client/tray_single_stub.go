//go:build !windows

package main

func acquireTraySingleInstance() (func(), bool, error) {
	return func() {}, true, nil
}
