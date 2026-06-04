package middleware

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery 捕获 handler 同步 panic,返回 500 不让进程崩溃
// 同时打 log 记录 panic 信息和堆栈
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC] %s %s -> %v\n%s", c.Request.Method, c.Request.URL.Path, err, debug.Stack())
				// 避免已经写过 response header
				if !c.Writer.Written() {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": fmt.Sprintf("Internal server error: %v", err),
					})
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
