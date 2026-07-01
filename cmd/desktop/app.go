package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"deploy-manager/pkg/rdp"
)

type App struct {
	ctx     context.Context
	sigChan chan os.Signal
}

func NewApp() *App {
	return &App{
		sigChan: make(chan os.Signal, 1),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	signal.Notify(a.sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Desktop app started")
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
