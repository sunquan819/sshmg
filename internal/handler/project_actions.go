package handler

import (
	"deploy-manager/internal/config"
	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"
	sshPkg "deploy-manager/pkg/ssh"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	// 如果组件从未部署过，且请求里也没指定服务器，则拒绝
	if component.DeployedServers == "" && req.ServerIDs == nil {
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
		log.Printf("[ComponentAction] log action, LogCmd=%s, Tail=%s, Offset=%d", component.LogCmd, req.Tail, req.Offset)
		tailLines := req.Tail
		if tailLines == "" {
			tailLines = "100"
		}
		// 去掉 -f / -F 等持续跟踪参数,避免 tail 阻塞 SSH 连接
		reFollow := regexp.MustCompile(`(^|\s)-[fF](\s|$)`)
		cmd = reFollow.ReplaceAllString(cmd, " ")
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
		// offset>0 表示要分页加载：用 head -n (offset+tail) | tail -n tail
		// 取"跳过前 offset 行、接下来的 tail 行",这样"更多"按钮能真正翻页
		// 注意：不要用子 shell 括号,SSH 的 bash -c 在 timeout 30 (...) 这种嵌套下解析不稳定
		if req.Offset > 0 {
			endLine := req.Offset + parsePositiveInt(tailLines, 100)
			cmd = fmt.Sprintf("%s 2>&1 | head -n %d | tail -n %d", cmd, endLine, endLine-req.Offset)
		}
		// 用 timeout 包裹整个命令,即使去掉了 -f 也兜底防止意外阻塞
		cmd = "timeout 30 " + cmd
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
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[PANIC ComponentAction server=%s] %v", s.Name, err)
				}
			}()

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
			// log action 内部已用 timeout 命令兜底，给 60s 给 SSH 留余量；其他 action 保持 30s
			sshTimeout := 30 * time.Second
			if req.Action == "log" {
				sshTimeout = 60 * time.Second
			}
			result, err := sshClient.Execute(execCmd, sshTimeout)

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

	// 日志弹窗不再做硬性截断（之前 100KB 截断会让"刷新最新"看到的是被截断的位置而不是文件末尾），
	// 改为只对单次响应做安全上限：1MB 之内直接返回；超过 1MB 截断并提示
	maxLogSize := 1 * 1024 * 1024
	truncated := false
	if len(allOutput) > maxLogSize {
		allOutput = allOutput[:maxLogSize] + "\n\n... (本批日志超过 1MB 已截断，请用「更多」继续加载)"
		truncated = true
	}

	c.JSON(http.StatusOK, gin.H{
		"output":         allOutput,
		"action":         req.Action,
		"server_count":   len(servers),
		"offset":         req.Offset,
		"truncated":      truncated,
	})
}

// parsePositiveInt 把字符串解析成正整数，解析失败时返回 fallback
func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// extractGrepKeyword 从 StatusCmd 中提取最后一个 grep 的关键字
// 例如 "ps -ef | grep seatone-bd-standard" -> "seatone-bd-standard"
// 例如 "ps -ef | grep -v grep | grep X" -> "X"
// 没找到就返回空字符串
func extractGrepKeyword(cmd string) string {
	// 按 | 拆管道，找最后一个 grep
	parts := strings.Split(cmd, "|")
	var lastKeyword string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "grep") {
			continue
		}
		// 提取 grep 后面的非 flag 参数
		fields := strings.Fields(p)
		for i, f := range fields {
			if i == 0 {
				continue // "grep" 本身
			}
			// 跳过 flag
			if strings.HasPrefix(f, "-") {
				// 如果是带值的 flag(如 -f X),跳过下一个
				if f == "-e" || f == "-f" {
					_ = i // placeholder
					if i+1 < len(fields) {
						// 不能修改 i，跳过
					}
				}
				continue
			}
			lastKeyword = f
		}
	}
	return lastKeyword
}

// shellQuote 用单引号包裹字符串,内部的单引号转义
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// CheckRunning 检查组件在所有目标服务器上是否正在运行
// 检测优先级（任一检测出 running 进程就判定 running=true）：
//   1. 用户配置的 StatusCmd：跑完整命令，看 ExitCode==0
//   2. StatusCmd 失败/未配 → fallback 用 pgrep -fa <从 StatusCmd 提取的 grep 关键字>
//   3. 还不行 → fallback 用 pgrep -fa <从 VersionCmd 提取的 grep 关键字>
//   4. 还不行 → fallback 用 pgrep -fa <组件名>
func (h *ProjectHandler) CheckRunning(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	// 决定目标服务器：DeployedServers > server_ids
	var serverIDs []uint
	if component.DeployedServers != "" {
		for _, sid := range strings.Split(component.DeployedServers, ",") {
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			if parsed, err := strconv.ParseUint(sid, 10, 32); err == nil {
				serverIDs = append(serverIDs, uint(parsed))
			}
		}
	}
	if len(serverIDs) == 0 && component.ServerIDs != "" {
		var configured []uint
		if err := json.Unmarshal([]byte(component.ServerIDs), &configured); err == nil {
			serverIDs = append(serverIDs, configured...)
		}
	}

	if len(serverIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用的目标服务器"})
		return
	}

	var servers []model.Server
	if err := database.DB.Find(&servers, serverIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Servers not found"})
		return
	}

	// 决定 fallback 关键字：StatusCmd 提取 > VersionCmd 提取 > 组件名
	fallbackKeyword := ""
	if component.StatusCmd != "" {
		fallbackKeyword = extractGrepKeyword(component.StatusCmd)
	}
	if fallbackKeyword == "" && component.VersionCmd != "" {
		fallbackKeyword = extractGrepKeyword(component.VersionCmd)
	}
	if fallbackKeyword == "" {
		fallbackKeyword = component.Name
	}

	type checkResult struct {
		ServerID uint   `json:"server_id"`
		ServerIP string `json:"server_ip"`
		Running  bool   `json:"running"`
		Output   string `json:"output"`
	}
	results := make([]checkResult, 0, len(servers))
	runningCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, s := range servers {
		wg.Add(1)
		go func(srv model.Server) {
			defer wg.Done()
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[PANIC CheckRunning server=%s] %v", srv.Name, err)
				}
			}()

			running := false
			out := ""

			// 方式 1:用户配置的 StatusCmd
			if component.StatusCmd != "" {
				execCmd := "cd " + component.DeployDir + " && " + component.StatusCmd
				result, _ := service.SSHSvc.RunCommand(&srv, execCmd, 15*time.Second)
				if result != nil {
					out = strings.TrimSpace(result.Output)
					if result.ExitCode == 0 {
						running = true
					}
				}
			}

			// 方式 2/3/4:pgrep fallback（StatusCmd 失败或没配时）
			if !running && fallbackKeyword != "" {
				pgrepCmd := "pgrep -fa " + shellQuote(fallbackKeyword) + " 2>/dev/null"
				pgrepResult, _ := service.SSHSvc.RunCommand(&srv, pgrepCmd, 5*time.Second)
				if pgrepResult != nil {
					// 过滤掉:
					//   - pgrep 自身进程（即使 pgrep 默认排除,但加保险）
					//   - bash 子进程 (用户的检测命令会临时启动 bash -c "..." 来跑命令)
					//   - grep 子进程 (StatusCmd 里 ps | grep X 跑时产生的子 grep)
					// 只把"真正的服务进程"算 running
					for _, line := range strings.Split(pgrepResult.Output, "\n") {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						// 跳过 pgrep 自身
						if strings.HasPrefix(line, "pgrep ") || strings.Contains(line, " /pgrep ") || strings.Contains(line, "pgrep -fa") {
							continue
						}
						// 跳过 bash -c 子进程（用户的检测命令通常在 bash -c 下跑）
						if strings.Contains(line, "bash -c") {
							continue
						}
						// 跳过 grep 子进程（ps -ef | grep X 跑出的 grep 自身）
						if strings.Contains(line, "grep "+fallbackKeyword) || strings.HasSuffix(line, "grep "+fallbackKeyword) {
							continue
						}
						// 找到真正的运行进程
						running = true
						if out != "" {
							out += "\n\n[fallback pgrep 找到进程: " + line + "]"
						} else {
							out = "[fallback pgrep 找到进程: " + line + "]"
						}
						break
					}
				}
			}

			mu.Lock()
			results = append(results, checkResult{ServerID: srv.ID, ServerIP: srv.IP, Running: running, Output: out})
			if running {
				runningCount++
			}
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"running_count":     runningCount,
		"server_count":      len(servers),
		"results":           results,
		"any_running":       runningCount > 0,
		"fallback_keyword":  fallbackKeyword,
		"used_fallback":     component.StatusCmd == "",
	})
}

type DeployUpdateRequest struct {
	ServerIDs any `json:"server_ids" binding:"required"`
}

// DeployUpdate 走"停止 → 备份 → 替换 → 启动"流程
// 对每台服务器：
//   1. 跑 StatusCmd 判断是否在运行
//   2. 如果在运行：跑 StopCmd
//   3. 备份 install_pkg 里的文件（加 .bak.YYYYMMDD-HHMMSS 后缀）
//   4. 上传新的 install_pkg
//   5. 跑 InstallCmd（可选）
//   6. 跑 StartCmd
//   7. 跑 StatusCmd 验证启动结果

// 部署日志裁剪配置:从 config.GlobalConfig.Deploy 读取,带默认值

// truncateDeployLog 把 append 后的全量日志裁剪成"最近 N 次 + 字节上限"
// 配置项来自 config.yaml 的 deploy 块(可空,空时用默认值)
func truncateDeployLog(full string) string {
	keepLast := 5
	maxBytes := 1 * 1024 * 1024
	sep := "====== 开始更新部署 ======"
	if config.GlobalConfig != nil {
		keepLast = config.GlobalConfig.Deploy.EffectiveLogKeepLast()
		maxBytes = config.GlobalConfig.Deploy.EffectiveLogMaxBytes()
		sep = config.GlobalConfig.Deploy.EffectiveLogSeparator()
	}

	// 1. 按部署块分隔符切分
	parts := strings.Split(full, sep)
	// parts[0] 是第一段的头部(可能为空),parts[1..] 是每次部署的正文(以分隔符开头)
	// 保留最后 keepLast 段
	if len(parts) > keepLast+1 {
		parts = parts[len(parts)-keepLast-1:]
	}
	kept := strings.Join(parts, sep)

	// 2. 字节上限(只在超过时截断)
	if len(kept) > maxBytes {
		// 保留尾部 maxBytes 字节,前面加截断标记
		truncated := "...(已截断更早的部署日志)...\n" + kept[len(kept)-(maxBytes-100):]
		return truncated
	}
	return kept
}
func (h *ProjectHandler) DeployUpdate(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var req DeployUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var serverIDs []uint
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
	if len(serverIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No servers selected"})
		return
	}

	var servers []model.Server
	if err := database.DB.Find(&servers, serverIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Servers not found"})
		return
	}
	if len(servers) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Servers not found"})
		return
	}

	// 性能优化:异步执行部署,立即 202 返回,避免 HTTP 请求阻塞 1+ 分钟
	// 前端轮询 GET /api/projects/components/:id/deploy-status 查看进度
	// 先把 status 设为 "running" 让前端立刻能看到状态变化
	database.DB.Model(&model.ProjectComponent{}).Where("id = ?", component.ID).Update("status", "running")

	service.SafeGo("DeployUpdate.do", func() {
		h.doDeployUpdate(&component, serverIDs)
	})

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Deployment started in background",
		"status":  "running",
	})
}

// doDeployUpdate 是 DeployUpdate 的实际逻辑(原同步函数体重构)
// 异步跑:在 goroutine 内执行 SSH 流程,结果写回 DB
func (h *ProjectHandler) doDeployUpdate(component *model.ProjectComponent, serverIDs []uint) {
	var servers []model.Server
	if err := database.DB.Find(&servers, serverIDs).Error; err != nil {
		log.Printf("[DeployUpdate] servers not found id=%d: %v", component.ID, err)
		return
	}

	var project model.Project
	_ = database.DB.First(&project, component.ProjectID).Error

	logTime := time.Now().Format("2006-01-02 15:04:05")
	timestamp := time.Now().Format("20060102-150405")
	var logContent string
	logContent = "[" + logTime + "] ====== 开始更新部署 ======\n"
	logContent += "[" + logTime + "] 组件: " + component.Name + "\n"
	logContent += "[" + logTime + "] 类型: " + component.Type + "\n"
	logContent += "[" + logTime + "] 版本: " + component.Version + "\n"
	logContent += "[" + logTime + "] 时间戳: " + timestamp + "\n"

	type stepResult struct {
		ServerID uint   `json:"server_id"`
		ServerIP string `json:"server_ip"`
		Step     string `json:"step"`
		Output   string `json:"output"`
		Err      string `json:"error,omitempty"`
	}
	results := make([]stepResult, 0)
	var mu sync.Mutex
	succeededIDs := make([]uint, 0, len(servers))
	failedCount := 0
	var wg sync.WaitGroup

	for _, server := range servers {
		wg.Add(1)
		go func(srv model.Server) {
			defer wg.Done()
			serverOK := true
			var serverErr string
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[PANIC DeployUpdate server=%s] %v", srv.Name, err)
					serverOK = false
					serverErr = "panic recovered"
				}
			}()

			logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 服务器 " + srv.Name + " (" + srv.IP + ") ======\n"

			// 1. 检查服务是否在运行
			wasRunning := false
			if component.StatusCmd != "" {
				execCmd := "cd " + component.DeployDir + " && " + component.StatusCmd
				result, _ := service.SSHSvc.RunCommand(&srv, execCmd, 15*time.Second)
				wasRunning = result != nil && result.ExitCode == 0
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 当前状态: " + map[bool]string{true: "运行中", false: "未运行"}[wasRunning] + "\n"
			}

			// 2. 如果在运行，先停止
			if wasRunning && component.StopCmd != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 停止服务...\n"
				stopCmd := "cd " + component.DeployDir + " && " + component.StopCmd
				stopResult, err := service.SSHSvc.RunCommand(&srv, stopCmd, 60*time.Second)
				if err != nil {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 停止服务失败 - " + err.Error() + "\n"
				} else {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 停止命令完成 (exit " + strconv.Itoa(stopResult.ExitCode) + ")\n"
				}
				mu.Lock()
				results = append(results, stepResult{ServerID: srv.ID, ServerIP: srv.IP, Step: "stop", Output: stopResult.Output})
				mu.Unlock()
			}

			// 3. 备份现有 install_pkg 里的文件（如果存在）
			if component.InstallPkg != "" {
				files := strings.Split(component.InstallPkg, ",")
				for _, f := range files {
					f = strings.TrimSpace(f)
					if f == "" {
						continue
					}
					remoteFilename := filepath.Base(f)
					remotePath := component.DeployDir + "/" + remoteFilename
					backupPath := remotePath + ".bak." + timestamp

					// 用 test -e 检查文件是否存在
					checkCmd := fmt.Sprintf(`test -e %q && echo "exists" || echo "notexists"`, remotePath)
					checkResult, _ := service.SSHSvc.RunCommand(&srv, checkCmd, 5*time.Second)
					if checkResult != nil && strings.TrimSpace(checkResult.Output) == "exists" {
						cpCmd := fmt.Sprintf("cp -f %q %q", remotePath, backupPath)
						cpResult, err := service.SSHSvc.RunCommand(&srv, cpCmd, 10*time.Second)
						if err != nil {
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 备份失败 " + remoteFilename + " - " + err.Error() + "\n"
						} else {
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 备份成功: " + remoteFilename + " → " + filepath.Base(backupPath) + "\n"
						}
						mu.Lock()
						results = append(results, stepResult{ServerID: srv.ID, ServerIP: srv.IP, Step: "backup:" + remoteFilename, Output: cpResult.Output})
						mu.Unlock()
					} else {
						logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 跳过备份(文件不存在): " + remoteFilename + "\n"
					}
				}
			}

			// 4. 上传新包
			if component.InstallPkg != "" {
				files := strings.Split(component.InstallPkg, ",")
				for _, f := range files {
					f = strings.TrimSpace(f)
					if f == "" {
						continue
					}
					var localPath string
					remoteFilename := filepath.Base(f)
					if strings.Contains(f, "/") || strings.Contains(f, "\\") {
						f = strings.TrimPrefix(f, "/")
						localPath = filepath.Join("./artifacts", f)
					} else {
						localPath = componentPackagePath(&project, component, f)
					}
					remotePath := component.DeployDir + "/" + remoteFilename
					if _, err := os.Stat(localPath); err == nil {
						if err := service.SSHSvc.UploadFile(&srv, localPath, remotePath); err != nil {
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 上传失败 " + remoteFilename + " - " + err.Error() + "\n"
							serverOK = false
							serverErr = "上传失败: " + err.Error()
						} else {
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 上传成功: " + remoteFilename + "\n"
						}
					} else {
						logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 跳过(本地文件不存在): " + localPath + "\n"
					}
				}
			}

			// 5. 跑 InstallCmd
			if serverOK && component.InstallCmd != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 执行安装命令...\n"
				installCmd := "cd " + component.DeployDir + " && " + component.InstallCmd
				installResult, err := service.SSHSvc.RunCommand(&srv, installCmd, 300*time.Second)
				if err != nil {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 警告: 安装命令执行异常 - " + err.Error() + "\n"
				} else {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 安装命令完成 (exit " + strconv.Itoa(installResult.ExitCode) + ")\n"
				}
				mu.Lock()
				results = append(results, stepResult{ServerID: srv.ID, ServerIP: srv.IP, Step: "install", Output: installResult.Output})
				mu.Unlock()
			}

			// 6. 跑 StartCmd
			if serverOK && component.StartCmd != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 启动服务...\n"
				startCmd := "cd " + component.DeployDir + " && " + component.StartCmd
				startResult, err := service.SSHSvc.RunCommand(&srv, startCmd, 60*time.Second)
				if err != nil {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 启动失败 - " + err.Error() + "\n"
					serverOK = false
					serverErr = "启动失败: " + err.Error()
				} else {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 启动命令完成 (exit " + strconv.Itoa(startResult.ExitCode) + ")\n"
				}
				mu.Lock()
				results = append(results, stepResult{ServerID: srv.ID, ServerIP: srv.IP, Step: "start", Output: startResult.Output})
				mu.Unlock()
			}

			if serverOK {
				succeededIDs = append(succeededIDs, srv.ID)
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 服务器 " + srv.Name + " 更新完成 ✓\n"
			} else {
				failedCount++
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 服务器 " + srv.Name + " 更新失败 ✗ (" + serverErr + ")\n"
			}
		}(server)
	}
	wg.Wait()

	logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 更新汇总 ======\n"
	logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 成功: " + strconv.Itoa(len(succeededIDs)) + "/" + strconv.Itoa(len(servers)) + " 台\n"
	if failedCount > 0 {
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 失败: " + strconv.Itoa(failedCount) + " 台\n"
	}

	switch {
	case len(succeededIDs) == len(servers):
		component.Status = "deployed"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 组件状态: 已部署\n"
	case len(succeededIDs) == 0:
		component.Status = "failed"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 组件状态: 部署失败\n"
	default:
		component.Status = "partial"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 组件状态: 部分部署\n"
	}

	// 跟 DeployComponent 保持一致:DeployedServers 不在这里覆盖
	// 保持跟用户配置的 server_ids 一致,部署结果通过 status 和 deploy_log 体现
	// （UpdateComponent 已经把 DeployedServers 同步成 server_ids,这里不动它）

	logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 更新流程结束 ======\n"

	if component.DeployLog != "" {
		component.DeployLog = truncateDeployLog(logContent + "\n" + component.DeployLog)
	} else {
		component.DeployLog = truncateDeployLog(logContent)
	}

	// 显式 Updates(map) 模式列出所有要更新的字段,避免 db.Save 在某些 GORM 版本下
	// 误把非零字段当成零值而漏写
	log.Printf("[DeployUpdate] id=%d status=%q deployed_servers=%q deploy_log_len=%d",
		component.ID, component.Status, component.DeployedServers, len(component.DeployLog))
	if err := database.DB.Model(&model.ProjectComponent{}).Where("id = ?", component.ID).Updates(map[string]interface{}{
		"status":           component.Status,
		"deployed_servers": component.DeployedServers,
		"deploy_log":       component.DeployLog,
	}).Error; err != nil {
		log.Printf("[DeployUpdate] id=%d write deploy_log failed: %v", component.ID, err)
		return
	}

	// 写完后再读一次 DB,确认 deploy_log 真的写进去了
	var storedLen int
	database.DB.Raw(`SELECT length(deploy_log) FROM project_components WHERE id = ?`, component.ID).Scan(&storedLen)
	log.Printf("[DeployUpdate] id=%d AFTER UPDATE deploy_log_len_in_db=%d", component.ID, storedLen)
}

// GetDeployStatus 返回组件当前部署状态 + 最近 deploy_log 一段
// 前端轮询这个端点判断部署是否结束(running -> deployed/failed)
func (h *ProjectHandler) GetDeployStatus(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	// 返回最新 deploy_log 末尾 16KB(供前端"看进度"用,不用拉全量)
	logTail := component.DeployLog
	const tailSize = 16 * 1024
	if len(logTail) > tailSize {
		logTail = logTail[len(logTail)-tailSize:]
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           component.Status,
		"deployed_servers": component.DeployedServers,
		"log_tail":         logTail,
		"log_full_len":     len(component.DeployLog),
		"is_running":       component.Status == "running",
	})
}
