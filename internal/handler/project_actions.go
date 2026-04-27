package handler

import (
	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	sshPkg "deploy-manager/pkg/ssh"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ComponentActionRequest struct {
	Action    string `json:"action" binding:"required"`
	ServerIDs any    `json:"server_ids"`
	Tail      string `json:"tail"`
	Offset    int    `json:"offset"`
}

func (h *ProjectHandler) ComponentAction(c *gin.Context) {
	componentID := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var req ComponentActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[ComponentAction] component ID=%d, LogCmd=%s, StartCmd=%s, StatusCmd=%s", component.ID, component.LogCmd, component.StartCmd, component.StatusCmd)

	if component.DeployedServers == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Component not deployed"})
		return
	}

	var serverIDs []uint
	if req.ServerIDs != nil {
		switch v := req.ServerIDs.(type) {
		case []interface{}:
			for _, id := range v {
				switch idVal := id.(type) {
				case float64:
					serverIDs = append(serverIDs, uint(idVal))
				case int:
					serverIDs = append(serverIDs, uint(idVal))
				case string:
					if parsed, err := strconv.ParseUint(idVal, 10, 32); err == nil {
						serverIDs = append(serverIDs, uint(parsed))
					}
				}
			}
		case []uint:
			serverIDs = v
		}
	}

	if len(serverIDs) == 0 {
		for _, sid := range strings.Split(component.DeployedServers, ",") {
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			id, err := strconv.ParseUint(sid, 10, 32)
			if err == nil {
				serverIDs = append(serverIDs, uint(id))
			}
		}
	}

	if len(serverIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No servers to operate on"})
		return
	}

	var servers []model.Server
	if err := database.DB.Find(&servers, serverIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Servers not found"})
		return
	}

	var cmd string
	switch req.Action {
	case "start":
		cmd = component.StartCmd
	case "stop":
		cmd = component.StopCmd
	case "status":
		cmd = component.StatusCmd
	case "log":
		cmd = component.LogCmd
		log.Printf("[ComponentAction] log action, LogCmd=%s, Tail=%s", component.LogCmd, req.Tail)
		tailLines := req.Tail
		if tailLines == "" {
			tailLines = "100"
		}
		reTail := regexp.MustCompile(`--tail=\d+`)
		reN := regexp.MustCompile(`-n\s+\d+`)
		if reTail.MatchString(cmd) {
			cmd = reTail.ReplaceAllString(cmd, "--tail="+tailLines)
		} else if reN.MatchString(cmd) {
			cmd = reN.ReplaceAllString(cmd, "-n "+tailLines)
		} else if cmd != "" {
			if strings.HasPrefix(strings.TrimSpace(cmd), "docker logs") {
				cmd = cmd + " --tail=" + tailLines
			} else {
				cmd = cmd + " | tail -n " + tailLines
			}
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action"})
		return
	}

	log.Printf("[ComponentAction] action=%s, cmd=%s", req.Action, cmd)

	if cmd == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Command not configured"})
		return
	}

	var wg sync.WaitGroup
	resultChan := make(chan string, len(servers))

	for _, server := range servers {
		wg.Add(1)
		go func(s model.Server) {
			defer wg.Done()

			sshClient := sshPkg.NewClient(s.IP, s.Port, s.Username, s.Password, s.SSHKey)
			sshClient.JumpEnabled = s.JumpEnabled
			sshClient.JumpHost = s.JumpIP
			sshClient.JumpPort = s.JumpPort
			sshClient.JumpUser = s.JumpUser
			sshClient.JumpPassword = s.JumpPassword
			sshClient.JumpKey = s.JumpKey

			if err := sshClient.Connect(); err != nil {
				resultChan <- "[" + s.Name + "] SSH连接失败: " + err.Error() + "\n"
				return
			}
			defer sshClient.Close()

			execCmd := "cd " + component.DeployDir + " && " + cmd
			result, err := sshClient.Execute(execCmd, 30*time.Second)

			var output string
			if err != nil {
				output = "[" + s.Name + "] 执行失败: " + err.Error() + "\n"
			} else if result != nil && result.Output != "" {
				output = "[" + s.Name + "] " + result.Output + "\n"
			} else {
				output = "[" + s.Name + "] 执行成功 (无输出)\n"
			}
			resultChan <- output
		}(server)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var allOutput string
	for r := range resultChan {
		allOutput += r
	}

	// 限制日志输出大小，最多返回 100KB
	maxLogSize := 100 * 1024
	if len(allOutput) > maxLogSize {
		allOutput = allOutput[:maxLogSize] + "\n\n... (日志过长，已截断)"
	}

	c.JSON(http.StatusOK, gin.H{"output": allOutput, "action": req.Action, "server_count": len(servers)})
}
