package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) setupTray() {
	runtime.EventsOn(a.ctx, "window:close", func(...interface{}) {
		runtime.WindowHide(a.ctx)
	})
}

func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
