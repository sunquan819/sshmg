package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"deploy-manager/pkg/assets"
	"deploy-manager/pkg/server"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to find available port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := server.Options{
		Listener:          listener,
		Desktop:           true,
		WebAssets:         assets.WebAssets,
		RDPAgentExe:       assets.RDPAgentExe,
		DefaultConfig:     assets.DefaultConfig,
		DefaultScenarios:  assets.DefaultScenarios,
		DefaultTemplates:  assets.DefaultTemplates,
	}

	go func() {
		if _, err := server.Start(ctx, opts); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port))
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	app := NewApp()

	err = wails.Run(&options.App{
		Title:     "Deploy Manager",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Handler: proxyWithTimeout(proxy),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatalf("Wails error: %v", err)
	}
}

func proxyWithTimeout(proxy *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

