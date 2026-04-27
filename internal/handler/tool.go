package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type ToolHandler struct{}

func NewToolHandler() *ToolHandler {
	return &ToolHandler{}
}

type ToolRequest struct {
	ServerID uint   `json:"server_id" binding:"required"`
	Tool     string `json:"tool" binding:"required"`
	Target   string `json:"target" binding:"required"`
	Options  string `json:"options"`
}

func (h *ToolHandler) ExecTool(c *gin.Context) {
	var req ToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, req.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	command := buildToolCommand(req.Tool, req.Target, req.Options)
	if command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool"})
		return
	}

	timeout := 60 * time.Second
	if req.Tool == "tcpdump" {
		timeout = 35 * time.Second
	}
	output, err := service.SSHSvc.ExecuteCommand(&server, command, timeout)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"output": "", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"output": output})
}

func buildToolCommand(tool, target, options string) string {
	switch tool {
	case "ping":
		opts := "-c 4"
		if options != "" {
			opts = options
		}
		return "ping " + opts + " " + target
	case "traceroute":
		opts := ""
		if options != "" {
			opts = " " + options
		}
		return "traceroute" + opts + " " + target
	case "telnet":
		return "timeout 5 telnet " + target
	case "curl":
		opts := "-s"
		if options != "" {
			opts = options
		}
		return "curl " + opts + " " + target
	case "nslookup":
		return "nslookup " + target
	case "dig":
		return "dig " + target
	case "host":
		return "host " + target
	case "nc":
		opts := "-zv"
		if options != "" {
			opts = options
		}
		return "nc " + opts + " " + target
	case "tcpdump":
		return buildTcpdumpCommand(target, options)
	default:
		return ""
	}
}

func buildTcpdumpCommand(target, options string) string {
	port := target
	filter := ""
	count := 100

	if options != "" {
		parts := parseToolOptions(options)
		if c, ok := parts["count"]; ok {
			fmt.Sscanf(c, "%d", &count)
		}
		if h, ok := parts["host"]; ok {
			filter = " and host " + h
		}
		if f, ok := parts["filter"]; ok {
			filter = " and " + f
		}
	}

	return fmt.Sprintf("timeout 30 tcpdump -i any -c %d port %s%s -nn", count, port, filter)
}

func parseToolOptions(options string) map[string]string {
	result := map[string]string{}
	parts := strings.Split(options, " ")
	for _, part := range parts {
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				result[kv[0]] = kv[1]
			}
		}
	}
	return result
}
