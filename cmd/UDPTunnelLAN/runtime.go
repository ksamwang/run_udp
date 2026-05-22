package main

import "sync"

var runtimeState struct {
	sync.RWMutex
	logPath string
}

func setRuntimeLogPath(path string) {
	runtimeState.Lock()
	defer runtimeState.Unlock()
	runtimeState.logPath = path
}

func runtimeLogPath() string {
	runtimeState.RLock()
	defer runtimeState.RUnlock()
	return runtimeState.logPath
}
