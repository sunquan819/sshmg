package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	sshPkg "deploy-manager/pkg/ssh"

	"github.com/gin-gonic/gin"
)

type InfrastructureHandler struct{}

func NewInfrastructureHandler() *InfrastructureHandler {
	return &InfrastructureHandler{}
}

type ScenarioRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Playbook    string `json:"playbook"`
	ServerIDs   any    `json:"server_ids"`
}

type ExecutionRequest struct {
	ScenarioID uint `json:"scenario_id" binding:"required"`
	ServerIDs  any  `json:"server_ids" binding:"required"`
}

func (h *InfrastructureHandler) ListScenarios(c *gin.Context) {
	var scenarios []model.InfrastructureScenario
	if err := database.DB.Order("created_at desc").Find(&scenarios).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scenarios": scenarios})
}

func (h *InfrastructureHandler) GetScenario(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	var scenario model.InfrastructureScenario
	if err := database.DB.First(&scenario, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	c.JSON(http.StatusOK, scenario)
}

func (h *InfrastructureHandler) CreateScenario(c *gin.Context) {
	var req ScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var serverIDsJSON string
	if req.ServerIDs != nil {
		data, _ := json.Marshal(req.ServerIDs)
		serverIDsJSON = string(data)
	}

	scenario := model.InfrastructureScenario{
		Name:        req.Name,
		Description: req.Description,
		Playbook:    req.Playbook,
		ServerIDs:   serverIDsJSON,
	}

	if err := database.DB.Create(&scenario).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, scenario)
}

func (h *InfrastructureHandler) UpdateScenario(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	var scenario model.InfrastructureScenario
	if err := database.DB.First(&scenario, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	var req ScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		body, _ := c.GetRawData()
		log.Printf("UpdateScenario bind error: %v, body: %s", err, string(body))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	scenario.Name = req.Name
	scenario.Description = req.Description
	scenario.Playbook = req.Playbook
	if req.ServerIDs != nil {
		data, _ := json.Marshal(req.ServerIDs)
		scenario.ServerIDs = string(data)
	}

	if err := database.DB.Save(&scenario).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, scenario)
}

func (h *InfrastructureHandler) DeleteScenario(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	if err := database.DB.Delete(&model.InfrastructureScenario{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *InfrastructureHandler) DeleteScenarioFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	filename := c.Param("filename")
	fileType := c.Query("type")

	var scenario model.InfrastructureScenario
	if err := database.DB.First(&scenario, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	uploadDir := filepath.Join(".", "uploads", "scenarios", strconv.FormatUint(id, 10), fileType)
	localPath := filepath.Join(uploadDir, filename)
	os.Remove(localPath)

	if fileType == "scripts" {
		files := strings.Split(scenario.ScriptFiles, ",")
		var newFiles []string
		for _, f := range files {
			if f != filename && f != "" {
				newFiles = append(newFiles, f)
			}
		}
		scenario.ScriptFiles = strings.Join(newFiles, ",")
	} else if fileType == "packages" {
		files := strings.Split(scenario.PackageFiles, ",")
		var newFiles []string
		for _, f := range files {
			if f != filename && f != "" {
				newFiles = append(newFiles, f)
			}
		}
		scenario.PackageFiles = strings.Join(newFiles, ",")
	}

	database.DB.Save(&scenario)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

type FileContentRequest struct {
	Content string `json:"content"`
}

func (h *InfrastructureHandler) GetScenarioFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	filename := c.Param("filename")
	fileType := c.Query("type")

	uploadDir := filepath.Join(".", "uploads", "scenarios", strconv.FormatUint(id, 10), fileType)
	localPath := filepath.Join(uploadDir, filename)

	data, err := os.ReadFile(localPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": string(data)})
}

func (h *InfrastructureHandler) UpdateScenarioFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	filename := c.Param("filename")
	fileType := c.Query("type")

	var req FileContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	uploadDir := filepath.Join(".", "uploads", "scenarios", strconv.FormatUint(id, 10), fileType)
	localPath := filepath.Join(uploadDir, filename)

	if err := os.WriteFile(localPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

func (h *InfrastructureHandler) UploadScenarioFiles(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario id"})
		return
	}

	var scenario model.InfrastructureScenario
	if err := database.DB.First(&scenario, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid form"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no files"})
		return
	}

	uploadDir := filepath.Join(".", "uploads", "scenarios", strconv.FormatUint(id, 10), c.Query("type"))
	os.MkdirAll(uploadDir, 0755)

	var uploadedFiles []string
	for _, file := range files {
		dst := filepath.Join(uploadDir, file.Filename)
		if err := c.SaveUploadedFile(file, dst); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		uploadedFiles = append(uploadedFiles, file.Filename)
	}

	fileList := strings.Join(uploadedFiles, ",")
	fileType := c.Query("type")
	if fileType == "scripts" {
		scenario.ScriptFiles = fileList
	} else if fileType == "packages" {
		scenario.PackageFiles = fileList
	}
	database.DB.Save(&scenario)

	c.JSON(http.StatusOK, gin.H{
		"message": "files uploaded",
		"files":   uploadedFiles,
	})
}

func (h *InfrastructureHandler) ExecuteScenario(c *gin.Context) {
	var req ExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var scenario model.InfrastructureScenario
	if err := database.DB.First(&scenario, req.ScenarioID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	var serverIDs []uint
	if ids, ok := req.ServerIDs.([]interface{}); ok {
		for _, id := range ids {
			if f, ok := id.(float64); ok {
				serverIDs = append(serverIDs, uint(f))
			}
		}
	}

	serverIDsJSON, _ := json.Marshal(serverIDs)
	execution := model.InfrastructureExecution{
		ScenarioID: req.ScenarioID,
		ServerIDs:  string(serverIDsJSON),
		Status:     "running",
	}
	database.DB.Create(&execution)

	go func() {
		h.runAnsible(scenario, serverIDs, execution.ID)
	}()

	c.JSON(http.StatusOK, gin.H{
		"execution_id": execution.ID,
		"status":       "running",
	})
}

func (h *InfrastructureHandler) runAnsible(scenario model.InfrastructureScenario, serverIDs []uint, executionID uint) {
	var output strings.Builder
	output.WriteString("===========================================\n")
	output.WriteString("  基础设施 - 批量任务执行\n")
	output.WriteString("===========================================\n\n")
	output.WriteString(fmt.Sprintf("场景: %s\n", scenario.Name))
	output.WriteString(fmt.Sprintf("命令: %s\n", scenario.Playbook))
	if scenario.ScriptFiles != "" {
		output.WriteString(fmt.Sprintf("脚本: %s\n", scenario.ScriptFiles))
	}
	if scenario.PackageFiles != "" {
		output.WriteString(fmt.Sprintf("安装包: %s\n", scenario.PackageFiles))
	}
	output.WriteString(fmt.Sprintf("服务器数量: %d\n", len(serverIDs)))
	output.WriteString(fmt.Sprintf("开始时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	var servers []model.Server
	database.DB.Where("id IN ?", serverIDs).Find(&servers)

	var wg sync.WaitGroup
	resultChan := make(chan string, len(servers))

	successCount := 0
	failCount := 0

	uploadDir := filepath.Join(".", "uploads", "scenarios", strconv.FormatUint(uint64(scenario.ID), 10))
	var scriptFiles []string
	var packageFiles []string
	if scenario.ScriptFiles != "" {
		scriptFiles = strings.Split(scenario.ScriptFiles, ",")
	}
	if scenario.PackageFiles != "" {
		packageFiles = strings.Split(scenario.PackageFiles, ",")
	}
	allFiles := append(scriptFiles, packageFiles...)

	for _, server := range servers {
		wg.Add(1)
		go func(s model.Server) {
			defer wg.Done()
			var result strings.Builder
			result.WriteString(fmt.Sprintf("\n===========================================\n"))
			result.WriteString(fmt.Sprintf(">>> 服务器: %s (%s:%d)\n", s.Name, s.IP, s.Port))
			result.WriteString(fmt.Sprintf("    用户名: %s\n", s.Username))
			result.WriteString("===========================================\n")

			sshClient := sshPkg.NewClient(s.IP, s.Port, s.Username, s.Password, s.SSHKey)
			sshClient.JumpEnabled = s.JumpEnabled
			sshClient.JumpHost = s.JumpIP
			sshClient.JumpPort = s.JumpPort
			sshClient.JumpUser = s.JumpUser
			sshClient.JumpPassword = s.JumpPassword
			sshClient.JumpKey = s.JumpKey
			sshClient.ProxyEnabled = s.ProxyEnabled
			sshClient.ProxyType = s.ProxyType
			sshClient.ProxyHost = s.ProxyHost
			sshClient.ProxyPort = s.ProxyPort

			var connectErr error
			for retry := 0; retry < 3; retry++ {
				if err := sshClient.Connect(); err == nil {
					connectErr = nil
					break
				} else {
					connectErr = err
					if retry < 2 {
						result.WriteString(fmt.Sprintf("⚠️ SSH 连接失败，%d 秒后重试...\n", (retry+1)*5))
						time.Sleep(time.Duration(retry+1) * 5 * time.Second)
					}
				}
			}
			if connectErr != nil {
				result.WriteString(fmt.Sprintf("❌ SSH 连接失败: %s\n", connectErr.Error()))
				failCount++
				resultChan <- result.String()
				return
			}

			remoteDir := "$HOME/infrastructure_" + strconv.FormatUint(uint64(executionID), 10)
			execRes, _ := sshClient.Execute("mkdir -p "+remoteDir, 10*time.Second)
			if execRes != nil && execRes.ExitCode != 0 {
				result.WriteString(fmt.Sprintf("⚠️ 创建目录失败: %s\n", execRes.Output))
			}

			shouldReconnect := false
			for i, fname := range allFiles {
				if i > 0 {
					sshClient.Close()
					sshClient = sshPkg.NewClient(s.IP, s.Port, s.Username, s.Password, s.SSHKey)
					sshClient.JumpEnabled = s.JumpEnabled
					sshClient.JumpHost = s.JumpIP
					sshClient.JumpPort = s.JumpPort
					sshClient.JumpUser = s.JumpUser
					sshClient.JumpPassword = s.JumpPassword
					sshClient.JumpKey = s.JumpKey
					sshClient.ProxyEnabled = s.ProxyEnabled
					sshClient.ProxyType = s.ProxyType
					sshClient.ProxyHost = s.ProxyHost
					sshClient.ProxyPort = s.ProxyPort
					if err := sshClient.Connect(); err != nil {
						result.WriteString(fmt.Sprintf("⚠️ SSH重连失败: %s\n", err.Error()))
						failCount++
						resultChan <- result.String()
						return
					}
				}

				localPath := filepath.Join(uploadDir, "scripts", fname)
				if _, err := os.Stat(localPath); os.IsNotExist(err) {
					localPath = filepath.Join(uploadDir, "packages", fname)
				}
				result.WriteString(fmt.Sprintf("📂 本地路径: %s\n", localPath))
				result.WriteString(fmt.Sprintf("📂 远程目录: %s\n", remoteDir))
				data, err := os.ReadFile(localPath)
				if err != nil {
					result.WriteString(fmt.Sprintf("⚠️ 文件不存在 %s: %s (尝试路径: %s)\n", fname, localPath, localPath))
					shouldReconnect = true
					continue
				}

				result.WriteString(fmt.Sprintf("📤 上传文件: %s (大小: %d bytes)\n", fname, len(data)))

				remoteFile := remoteDir + "/" + fname
				nativeClient, err := sshClient.GetNativeClient()
				if err != nil {
					result.WriteString(fmt.Sprintf("❌ 获取SSH客户端失败: %s\n", err.Error()))
					shouldReconnect = true
					continue
				}
				session, err := nativeClient.NewSession()
				if err != nil {
					result.WriteString(fmt.Sprintf("❌ 创建会话失败 %s: %s\n", fname, err.Error()))
					shouldReconnect = true
					continue
				}

				stdin, err := session.StdinPipe()
				if err != nil {
					session.Close()
					result.WriteString(fmt.Sprintf("❌ 获取stdin失败 %s: %s\n", fname, err.Error()))
					shouldReconnect = true
					continue
				}

				err = session.Start(fmt.Sprintf("cat > %q", remoteFile))
				if err != nil {
					session.Close()
					result.WriteString(fmt.Sprintf("❌ 启动命令失败 %s: %s\n", fname, err.Error()))
					shouldReconnect = true
					continue
				}

				_, err = stdin.Write(data)
				if err != nil {
					stdin.Close()
					session.Close()
					result.WriteString(fmt.Sprintf("❌ 写入失败 %s: %s\n", fname, err.Error()))
					shouldReconnect = true
					continue
				}
				stdin.Close()

				err = session.Wait()
				session.Close()
				if err != nil {
					result.WriteString(fmt.Sprintf("❌ 上传失败 %s: %s\n", fname, err.Error()))
					shouldReconnect = true
					continue
				}

				execRes, _ := sshClient.Execute("ls -la "+remoteFile, 10*time.Second)
				result.WriteString(fmt.Sprintf("✅ 上传完成: %s\n", fname))
				if execRes != nil && execRes.Output != "" {
					result.WriteString(fmt.Sprintf("   验证: %s\n", execRes.Output))
				}
			}

			if shouldReconnect {
				sshClient.Close()
				sshClient = sshPkg.NewClient(s.IP, s.Port, s.Username, s.Password, s.SSHKey)
				sshClient.JumpEnabled = s.JumpEnabled
				sshClient.JumpHost = s.JumpIP
				sshClient.JumpPort = s.JumpPort
				sshClient.JumpUser = s.JumpUser
				sshClient.JumpPassword = s.JumpPassword
				sshClient.JumpKey = s.JumpKey
				sshClient.ProxyEnabled = s.ProxyEnabled
				sshClient.ProxyType = s.ProxyType
				sshClient.ProxyHost = s.ProxyHost
				sshClient.ProxyPort = s.ProxyPort
				if err := sshClient.Connect(); err != nil {
					result.WriteString(fmt.Sprintf("⚠️ SSH重连失败: %s\n", err.Error()))
					failCount++
					resultChan <- result.String()
					return
				}
			}

			sshClient.Close()
			time.Sleep(500 * time.Millisecond)
			sshClient = sshPkg.NewClient(s.IP, s.Port, s.Username, s.Password, s.SSHKey)
			sshClient.JumpEnabled = s.JumpEnabled
			sshClient.JumpHost = s.JumpIP
			sshClient.JumpPort = s.JumpPort
			sshClient.JumpUser = s.JumpUser
			sshClient.JumpPassword = s.JumpPassword
			sshClient.JumpKey = s.JumpKey
			sshClient.ProxyEnabled = s.ProxyEnabled
			sshClient.ProxyType = s.ProxyType
			sshClient.ProxyHost = s.ProxyHost
			sshClient.ProxyPort = s.ProxyPort
			if err := sshClient.Connect(); err != nil {
				result.WriteString(fmt.Sprintf("❌ SSH 重连失败: %s\n", err.Error()))
				failCount++
				resultChan <- result.String()
				return
			}

			result.WriteString("▶ 执行命令...\n\n")

			execCmd := scenario.Playbook
			if len(allFiles) > 0 {
				execCmd = "cd " + remoteDir + " && " + scenario.Playbook
			}

			execResult, err := sshClient.Execute(execCmd, 300*time.Second)
			if err != nil {
				result.WriteString(fmt.Sprintf("❌ 执行失败: %s\n", err.Error()))
				if execResult != nil {
					result.WriteString(fmt.Sprintf("   输出: %s\n", execResult.Output))
				}
				failCount++
			} else {
				result.WriteString(fmt.Sprintf("✅ 执行成功 (退出码: %d)\n", execResult.ExitCode))
				result.WriteString(fmt.Sprintf("\n--- 输出 ---\n%s\n", execResult.Output))
				successCount++
			}

			sshClient.Close()
			resultChan <- result.String()
		}(server)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for r := range resultChan {
		output.WriteString(r)
	}

	output.WriteString("\n===========================================\n")
	output.WriteString(fmt.Sprintf("完成时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	output.WriteString(fmt.Sprintf("总服务器数: %d\n", len(servers)))
	output.WriteString(fmt.Sprintf("成功: %d\n", successCount))
	output.WriteString(fmt.Sprintf("失败: %d\n", failCount))
	output.WriteString("===========================================\n")

	now := time.Now()
	database.DB.Model(&model.InfrastructureExecution{}).Where("id = ?", executionID).Updates(map[string]interface{}{
		"status":       "completed",
		"output":       output.String(),
		"completed_at": now,
	})
}

func (h *InfrastructureHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	var execution model.InfrastructureExecution
	if err := database.DB.First(&execution, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}
	c.JSON(http.StatusOK, execution)
}

func (h *InfrastructureHandler) ListExecutions(c *gin.Context) {
	var executions []model.InfrastructureExecution
	if err := database.DB.Order("created_at desc").Limit(50).Find(&executions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"executions": executions})
}
