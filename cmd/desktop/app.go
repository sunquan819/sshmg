package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"deploy-manager/pkg/rdp"
)

type App struct {
	ctx          context.Context
	sigChan      chan os.Signal
	settingsPath string
	settings     DesktopSettings
}

func NewApp(settingsPath string, settings DesktopSettings) *App {
	return &App{
		sigChan:      make(chan os.Signal, 1),
		settingsPath: settingsPath,
		settings:     settings,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	signal.Notify(a.sigChan, syscall.SIGINT, syscall.SIGTERM)
	a.setupTray()
	log.Println("Desktop app started")
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Desktop] heartbeat stopped: context done")
				return
			case <-ticker.C:
				log.Printf("[Desktop] heartbeat pid=%d", os.Getpid())
			}
		}
	}()
}

func (a *App) domReady(ctx context.Context) {
	log.Println("[Desktop] DOM ready")
}

func (a *App) beforeClose(ctx context.Context) bool {
	log.Println("[Desktop] before close requested")
	return false
}

func (a *App) shutdown(ctx context.Context) {
	log.Println("Desktop app shutting down")
}

func (a *App) LaunchRDP(host string, port int, username string, password string) error {
	return rdp.Launch(rdp.ConnectRequest{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	})
}

func (a *App) GetDesktopSettings() DesktopSettings {
	current := loadDesktopSettings(a.settingsPath)
	current.CurrentPort = a.settings.CurrentPort
	current.CurrentBindHost = a.settings.CurrentBindHost
	current.SettingsPath = a.settingsPath
	current.RestartRequired = current.FixedPort != a.settings.FixedPort || current.AllowLAN != a.settings.AllowLAN
	return current
}

func (a *App) SaveDesktopSettings(settings DesktopSettings) (DesktopSettings, error) {
	if settings.FixedPort < 0 || settings.FixedPort > 65535 {
		settings.FixedPort = 0
	}
	if err := saveDesktopSettings(a.settingsPath, settings); err != nil {
		return a.GetDesktopSettings(), err
	}
	return a.GetDesktopSettings(), nil
}
