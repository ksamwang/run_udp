//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type serviceRunner struct {
	run func(context.Context)
}

func (m *serviceRunner) Execute(args []string, req <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	changes <- svc.Status{State: svc.StartPending}
	go func() {
		defer close(done)
		m.run(ctx)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
}

func runWindowsService(run func(context.Context)) error {
	appRuntime.SetServiceStatus("starting")
	err := svc.Run(windowsServiceName, &serviceRunner{run: func(ctx context.Context) {
		appRuntime.SetServiceStatus("running")
		run(ctx)
		appRuntime.SetServiceStatus("stopped")
	}})
	if err != nil {
		appRuntime.SetServiceStatus("error")
	}
	return err
}

func installWindowsService(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(windowsServiceName); err == nil {
		defer s.Close()
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		cfg.DisplayName = windowsServiceName
		cfg.StartType = mgr.StartAutomatic
		cfg.BinaryPathName = serviceBinaryPath(exePath, configPath)
		if err := s.UpdateConfig(cfg); err != nil {
			return err
		}
		return setServiceRecovery(windowsServiceName)
	}
	s, err := m.CreateService(windowsServiceName, exePath, mgr.Config{
		DisplayName: windowsServiceName,
		StartType:   mgr.StartAutomatic,
	}, "-service", "-config", configPath)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := setServiceRecovery(windowsServiceName); err != nil {
		return err
	}
	return nil
}

func serviceBinaryPath(exePath, configPath string) string {
	return fmt.Sprintf("%q -service -config %q", exePath, configPath)
}

func uninstallWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return nil
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	time.Sleep(500 * time.Millisecond)
	return s.Delete()
}

func startWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.Start(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already running") {
		return err
	}
	return nil
}

func stopWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = s.Control(svc.Stop)
	return err
}

func restartWindowsService() error {
	_ = stopWindowsService()
	for i := 0; i < 40; i++ {
		st, _ := queryWindowsServiceStatus()
		if st == "stopped" || st == "missing" || st == "unknown" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return startWindowsService()
}

func queryWindowsServiceStatus() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "unknown", err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return "missing", nil
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "unknown", err
	}
	switch st.State {
	case svc.Running:
		return "running", nil
	case svc.Stopped:
		return "stopped", nil
	case svc.StartPending:
		return "start_pending", nil
	case svc.StopPending:
		return "stop_pending", nil
	default:
		return fmt.Sprintf("%d", st.State), nil
	}
}

func ensureTrayStartup(exePath, configPath string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cmd := fmt.Sprintf("%q -tray -config %q", exePath, configPath)
	return k.SetStringValue(windowsRunKeyName, cmd)
}

func spawnTrayHelper(exePath, configPath string) error {
	cmd := exec.Command(exePath, "-tray", "-config", configPath)
	cmd.Dir = filepath.Dir(exePath)
	return cmd.Start()
}

func removeTrayStartup() error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	_ = k.DeleteValue(windowsRunKeyName)
	return nil
}

func setServiceRecovery(name string) error {
	cmd := exec.Command("sc.exe", "failure", name, "reset=", "86400", "actions=", "restart/5000/restart/5000/restart/10000")
	return cmd.Run()
}

func openLogs() error {
	logPath := currentRuntimeInfo().LogPath
	if logPath == "" {
		return errors.New("log path unavailable")
	}
	return exec.Command("explorer.exe", filepath.Dir(logPath)).Start()
}

func spawnServiceCommand(arg string) error {
	return runElevatedSelf(arg)
}

func isServiceInteractive() bool {
	ok, err := svc.IsAnInteractiveSession()
	if err != nil {
		return true
	}
	return ok
}
