package service

import (
	"log"
	"runtime/debug"
)

// SafeGo 启动一个 goroutine 并捕获 panic,防止单个 goroutine 崩溃整个进程
// 所有异步任务(部署/批量执行/SSH 异步)都应使用此函数
// name 用于日志定位,func 是要执行的实际逻辑
func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC goroutine=%s] %v\n%s", name, err, debug.Stack())
			}
		}()
		fn()
	}()
}
