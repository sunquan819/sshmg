package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"deploy-manager/pkg/assets"
	"deploy-manager/pkg/server"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

var desktopLogFiles []*os.File

type DesktopSettings struct {
	FixedPort       int    `json:"fixedPort"`
	AllowLAN        bool   `json:"allowLAN"`
	CurrentPort     int    `json:"currentPort"`
	CurrentBindHost string `json:"currentBindHost"`
	SettingsPath    string `json:"settingsPath"`
	RestartRequired bool   `json:"restartRequired"`
}

func main() {
	logPath := setupDesktopLogging()
	defer closeDesktopLogs()
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[Desktop PANIC] %v\n%s", err, debug.Stack())
			panic(err)
		}
	}()
	log.Printf("[Desktop] log file: %s", logPath)

	settingsPath := desktopSettingsPath()
	settings := loadDesktopSettings(settingsPath)
	bindHost := "127.0.0.1"
	if settings.AllowLAN {
		bindHost = "0.0.0.0"
	}
	port := settings.FixedPort
	listenAddr := fmt.Sprintf("%s:%d", bindHost, port)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Printf("[Desktop] listen %s failed: %v, falling back to 127.0.0.1:0", listenAddr, err)
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatalf("Failed to find available port: %v", err)
		}
		bindHost = "127.0.0.1"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := server.Options{
		Listener:         listener,
		Desktop:          true,
		WebAssets:        assets.WebAssets,
		RDPAgentExe:      assets.RDPAgentExe,
		DefaultConfig:    assets.DefaultConfig,
		DefaultScenarios: assets.DefaultScenarios,
		DefaultTemplates: assets.DefaultTemplates,
	}

	go func() {
		if _, err := server.Start(ctx, opts); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port))
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	actualPort := listener.Addr().(*net.TCPAddr).Port
	app := NewApp(settingsPath, DesktopSettings{
		FixedPort:       settings.FixedPort,
		AllowLAN:        settings.AllowLAN,
		CurrentPort:     actualPort,
		CurrentBindHost: bindHost,
		SettingsPath:    settingsPath,
	})

	err = wails.Run(&options.App{
		Title:     "Deploy Manager",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Handler: proxyWithTimeout(proxy),
		},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatalf("Wails error: %v", err)
	}
}

func desktopSettingsPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "desktop-settings.json"
	}
	return filepath.Join(filepath.Dir(exePath), "desktop-settings.json")
}

func loadDesktopSettings(path string) DesktopSettings {
	var settings DesktopSettings
	data, err := os.ReadFile(path)
	if err != nil {
		return settings
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		log.Printf("[Desktop] read settings failed: %v", err)
	}
	return settings
}

func saveDesktopSettings(path string, settings DesktopSettings) error {
	data, err := json.MarshalIndent(DesktopSettings{
		FixedPort: settings.FixedPort,
		AllowLAN:  settings.AllowLAN,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func setupDesktopLogging() string {
	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("Failed to create log directory %s: %v", logDir, err)
		return ""
	}
	latestPath := filepath.Join(logDir, "desktop-latest.log")
	dailyPath := filepath.Join(logDir, "desktop-"+time.Now().Format("20060102-150405")+".log")
	file, err := os.OpenFile(dailyPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to open desktop log %s: %v", dailyPath, err)
		return dailyPath
	}
	latest, latestErr := os.OpenFile(latestPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if latestErr != nil {
		log.Printf("Failed to open latest desktop log %s: %v", latestPath, latestErr)
	}
	desktopLogFiles = append(desktopLogFiles, file)
	writers := []io.Writer{file}
	if latestErr == nil {
		desktopLogFiles = append(desktopLogFiles, latest)
		writers = append(writers, latest)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.SetOutput(io.MultiWriter(writers...))
	return latestPath
}

func closeDesktopLogs() {
	for _, file := range desktopLogFiles {
		_ = file.Sync()
		_ = file.Close()
	}
	desktopLogFiles = nil
}

func proxyWithTimeout(proxy *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			proxy.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}
