package handler

import (
	"encoding/json"
	"fmt"
	"io"
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
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type ProjectHandler struct{}

func (h *ProjectHandler) ListProjects(c *gin.Context) {
	// 性能优化:不预加载 Components(避免 100MB+ 响应,deploy_log 字段最重)
	// 前端通过 GET /api/projects/:id 获取单项目 + 完整 components
	var projects []model.Project
	if err := database.DB.Order("created_at DESC").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 收集所有 project id,批量查 components(排除 deploy_log,1MB 上限已经能控住但
	// 列表不显示就不该传,留到 GetDeployLog 单独拉)
	projIDs := make([]uint, 0, len(projects))
	for _, p := range projects {
		projIDs = append(projIDs, p.ID)
	}

	type componentListItem struct {
		ID                 uint
		ProjectID          uint
		Name               string
		Type               string
		Version            string
		DeployDir          string
		StatusCmd          string
		LogCmd             string
		VersionCmd         string
		AccessUser         string
		AccessPassword     string
		AccessURL          string
		InstallPkg         string
		InstallCmd         string
		StartCmd           string
		StopCmd            string
		ConfigFile         string
		Status             string
		ServerIDs          string
		DeployedServers    string
		VersionsPerServer  string
		CreatedAt          time.Time
		UpdatedAt          time.Time
	}
	var comps []componentListItem
	if len(projIDs) > 0 {
		// 显式 Select 排除 deploy_log 字段
		if err := database.DB.Table("project_components").
			Select(`id, project_id, name, type, version, deploy_dir, status_cmd, log_cmd, version_cmd,
			        access_user, access_password, access_url, install_pkg, install_cmd, start_cmd, stop_cmd,
			        config_file, status, server_ids, deployed_servers, versions_per_server, created_at, updated_at`).
			Where("project_id IN ?", projIDs).
			Find(&comps).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 把 components 按 project_id 分到 projects[i].Components
	byProj := make(map[uint][]model.ProjectComponent, len(projects))
	for _, c := range comps {
		// 显式转回 model.ProjectComponent(不含 deploy_log,但前端列表用不到)
		byProj[c.ProjectID] = append(byProj[c.ProjectID], model.ProjectComponent{
			ID:                c.ID,
			ProjectID:         c.ProjectID,
			Name:              c.Name,
			Type:              c.Type,
			Version:           c.Version,
			DeployDir:         c.DeployDir,
			StatusCmd:         c.StatusCmd,
			LogCmd:            c.LogCmd,
			VersionCmd:        c.VersionCmd,
			AccessUser:        c.AccessUser,
			AccessPassword:    c.AccessPassword,
			AccessURL:         c.AccessURL,
			InstallPkg:        c.InstallPkg,
			InstallCmd:        c.InstallCmd,
			StartCmd:          c.StartCmd,
			StopCmd:           c.StopCmd,
			ConfigFile:        c.ConfigFile,
			Status:            c.Status,
			ServerIDs:         c.ServerIDs,
			DeployedServers:   c.DeployedServers,
			VersionsPerServer: c.VersionsPerServer,
			CreatedAt:         c.CreatedAt,
			UpdatedAt:         c.UpdatedAt,
		})
	}
	for i := range projects {
		projects[i].Components = byProj[projects[i].ID]
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func (h *ProjectHandler) GetProject(c *gin.Context) {
	id := c.Param("id")
	var project model.Project
	if err := database.DB.Preload("Components").First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": project})
}

type CreateProjectRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	project := model.Project{
		Name:        req.Name,
		Description: req.Description,
		Status:      "active",
	}

	if err := database.DB.Create(&project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"project": project})
}

func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	id := c.Param("id")
	var project model.Project
	if err := database.DB.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	project.Name = req.Name
	project.Description = req.Description

	if err := database.DB.Save(&project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"project": project})
}

func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	id := c.Param("id")
	if err := database.DB.Delete(&model.Project{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

type CreateComponentRequest struct {
	ProjectID      uint   `json:"project_id" binding:"required"`
	Name           string `json:"name" binding:"required"`
	Type           string `json:"type" binding:"required"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	DeployDir      string `json:"deploy_dir"`
	StatusCmd      string `json:"status_cmd"`
	LogCmd         string `json:"log_cmd"`
	VersionCmd     string `json:"version_cmd"`
	AccessUser     string `json:"access_user"`
	AccessPassword string `json:"access_password"`
	AccessURL      string `json:"access_url"`
	InstallCmd     string `json:"install_cmd"`
	StartCmd       string `json:"start_cmd"`
	StopCmd        string `json:"stop_cmd"`
	ConfigFile     string `json:"config_file"`
	ServerIDs      any    `json:"server_ids"`
	Status         string `json:"status"`
}

type UpdateComponentRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	DeployDir      string `json:"deploy_dir"`
	StatusCmd      string `json:"status_cmd"`
	LogCmd         string `json:"log_cmd"`
	VersionCmd     string `json:"version_cmd"`
	AccessUser     string `json:"access_user"`
	AccessPassword string `json:"access_password"`
	AccessURL      string `json:"access_url"`
	InstallCmd     string `json:"install_cmd"`
	StartCmd       string `json:"start_cmd"`
	StopCmd        string `json:"stop_cmd"`
	ConfigFile     string `json:"config_file"`
	InstallPkg     string `json:"install_pkg"`
	ServerIDs      any    `json:"server_ids"`
	Status         string `json:"status"`
}

func (h *ProjectHandler) CreateComponent(c *gin.Context) {
	var req CreateComponentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[CreateComponent] Bind error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[CreateComponent] req: %+v", req)

	var serverIDsJSON string
	if req.ServerIDs != nil {
		data, _ := json.Marshal(req.ServerIDs)
		serverIDsJSON = string(data)
	}

	component := model.ProjectComponent{
		ProjectID:      req.ProjectID,
		Name:           req.Name,
		Type:           req.Type,
		Description:    req.Description,
		Version:        req.Version,
		DeployDir:      req.DeployDir,
		StatusCmd:      req.StatusCmd,
		LogCmd:         req.LogCmd,
		VersionCmd:     req.VersionCmd,
		AccessUser:     req.AccessUser,
		AccessPassword: req.AccessPassword,
		AccessURL:      req.AccessURL,
		InstallCmd:     req.InstallCmd,
		StartCmd:       req.StartCmd,
		StopCmd:        req.StopCmd,
		ConfigFile:     req.ConfigFile,
		Status:         "not_deployed",
		ServerIDs:      serverIDsJSON,
	}

	if err := database.DB.Create(&component).Error; err != nil {
		log.Printf("[CreateComponent] DB error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"component": component})
}

func (h *ProjectHandler) UpdateComponent(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var req UpdateComponentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[UpdateComponent] id=%s, req.LogCmd=%s, current.LogCmd=%s", id, req.LogCmd, component.LogCmd)

	// 用 map 显式列出要更新的字段，避免 db.Save 在某些场景下把未在 req 里出现的
	// 字段（特别是 DeployedServers 这种状态字段）当成零值/旧值清掉
	updates := map[string]interface{}{}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Type != "" {
		updates["type"] = req.Type
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Version != "" {
		updates["version"] = req.Version
	}
	if req.DeployDir != "" {
		updates["deploy_dir"] = req.DeployDir
	}
	if req.StatusCmd != "" {
		updates["status_cmd"] = req.StatusCmd
	}
	if req.LogCmd != "" {
		updates["log_cmd"] = req.LogCmd
	}
	// VersionCmd 允许空字符串清空
	updates["version_cmd"] = req.VersionCmd
	if req.AccessUser != "" {
		updates["access_user"] = req.AccessUser
	}
	if req.AccessPassword != "" {
		updates["access_password"] = req.AccessPassword
	}
	if req.AccessURL != "" {
		updates["access_url"] = req.AccessURL
	}
	if req.InstallCmd != "" {
		updates["install_cmd"] = req.InstallCmd
	}
	if req.StartCmd != "" {
		updates["start_cmd"] = req.StartCmd
	}
	if req.StopCmd != "" {
		updates["stop_cmd"] = req.StopCmd
	}
	if req.ConfigFile != "" {
		updates["config_file"] = req.ConfigFile
	}
	// InstallPkg 允许空
	updates["install_pkg"] = req.InstallPkg

	if req.ServerIDs != nil {
		data, _ := json.Marshal(req.ServerIDs)
		updates["server_ids"] = string(data)

		// DeployedServers 直接同步为 server_ids：
		// 编辑时改了 server_ids,DeployedServers 跟着变,跟用户配置保持一致
		// 部署成功后,DeployComponent / DeployUpdate 会用"实际成功的 server id"覆盖
		updates["deployed_servers"] = serverIDsToCSV(string(data))
	}

	if req.Status != "" {
		updates["status"] = req.Status
		switch req.Status {
		case "deployed", "partial":
			// 手动标记为已部署/部分部署时，如果 DeployedServers 为空，
			// 从配置的 server_ids 复制过来，让"部署服务器"列能直接看到目标
			if component.DeployedServers == "" {
				// 优先用本次请求里要设置的 server_ids
				serverIDsJSON := component.ServerIDs
				if req.ServerIDs != nil {
					data, _ := json.Marshal(req.ServerIDs)
					serverIDsJSON = string(data)
				}
				if serverIDsJSON != "" {
					updates["deployed_servers"] = serverIDsToCSV(serverIDsJSON)
				}
			}
		case "not_deployed", "failed":
			// 改回未部署/失败时清空 DeployedServers
			updates["deployed_servers"] = ""
		}
	}

	if len(updates) == 0 {
		// 没东西要改，直接返回当前值
		c.JSON(http.StatusOK, gin.H{"component": component})
		return
	}

	if err := database.DB.Model(&component).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 重新读回，保证返回给前端的是 DB 里的最新值
	database.DB.First(&component, id)
	c.JSON(http.StatusOK, gin.H{"component": component})
}

// serverIDsToCSV 把 JSON 数组字符串（[1,2,3] 或 ["1","2"]）转成 CSV 字符串（"1,2,3"）
// server_id 可能是数字也可能是字符串（前端 JSON.parse 后 ID 可能是数字也可能是字符串），
// 这里都兼容。
func serverIDsToCSV(serverIDsJSON string) string {
	var raw []interface{}
	if err := json.Unmarshal([]byte(serverIDsJSON), &raw); err != nil {
		return ""
	}
	parts := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case float64:
			parts = append(parts, strconv.FormatUint(uint64(val), 10))
		case int:
			parts = append(parts, strconv.Itoa(val))
		case int64:
			parts = append(parts, strconv.FormatInt(val, 10))
		case string:
			if parsed, err := strconv.ParseUint(val, 10, 32); err == nil {
				parts = append(parts, strconv.FormatUint(parsed, 10))
			}
		}
	}
	return strings.Join(parts, ",")
}

func (h *ProjectHandler) DeleteComponent(c *gin.Context) {
	id := c.Param("id")
	if err := database.DB.Delete(&model.ProjectComponent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

type DeployComponentRequest struct {
	ServerIDs any `json:"server_ids" binding:"required"`
}

func (h *ProjectHandler) DeployComponent(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var req DeployComponentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var servers []model.Server
	var serverIDs []uint

	switch v := req.ServerIDs.(type) {
	case []interface{}:
		for _, id := range v {
			if f, ok := id.(float64); ok {
				serverIDs = append(serverIDs, uint(f))
			} else if i, ok := id.(int); ok {
				serverIDs = append(serverIDs, uint(i))
			}
		}
	case []uint:
		serverIDs = v
	case []int:
		for _, id := range v {
			serverIDs = append(serverIDs, uint(id))
		}
	}

	if len(serverIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No servers selected"})
		return
	}

	if err := database.DB.Find(&servers, serverIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Servers not found"})
		return
	}

	if len(servers) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Servers not found"})
		return
	}

	// 提前查一次 Project，后面解析本地安装包路径要用
	var project model.Project
	_ = database.DB.First(&project, component.ProjectID).Error

	logTime := time.Now().Format("2006-01-02 15:04:05")
	var logContent string
	logContent = "[" + logTime + "] ====== 开始部署 ======\n"
	logContent += "[" + logTime + "] 组件: " + component.Name + "\n"
	logContent += "[" + logTime + "] 类型: " + component.Type + "\n"
	logContent += "[" + logTime + "] 版本: " + component.Version + "\n"

	// 部署到所有服务器，跟踪每台服务器的成功/失败
	succeededIDs := make([]uint, 0, len(servers))
	failedCount := 0

	// 部署到所有服务器
	for idx, server := range servers {
		// 跟踪本服务器部署是否成功
		serverOK := true
		var serverErr string

		logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 服务器 " + strconv.Itoa(idx+1) + "/" + strconv.Itoa(len(servers)) + ": " + server.Name + " (" + server.IP + ") ======\n"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 连接: " + server.IP + ":" + strconv.Itoa(server.Port) + "\n"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 部署目录: " + component.DeployDir + "\n"

		// 1. 创建部署目录
		if component.DeployDir != "" {
			err := service.SSHSvc.RemoteMkdir(&server, component.DeployDir)
			if err != nil {
				serverOK = false
				serverErr = "创建目录失败: " + err.Error()
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + serverErr + "\n"
			} else {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 目录创建成功\n"
			}
		}

		// 2. 上传安装包（如果有安装包且目录创建成功才上传，避免无谓错误）
		if serverOK {
			if component.InstallPkg != "" {
				files := strings.Split(component.InstallPkg, ",")
				hasFiles := false
				allUploaded := true
				for _, f := range files {
					f = strings.TrimSpace(f)
					if f == "" {
						continue
					}
					hasFiles = true

					var localPath string
					remoteFilename := filepath.Base(f)

				// 判断是否是完整路径（包含分隔符）
				if strings.Contains(f, "/") || strings.Contains(f, "\\") {
					// 完整路径：直接拼接 artifacts 目录
					f = strings.TrimPrefix(f, "/")
					localPath = filepath.Join("./artifacts", f)
				} else {
					// 传统格式：只有文件名，优先从项目目录找，兼容老的 component_<id> 目录
					localPath = componentPackagePath(&project, &component, f)
					if _, err := os.Stat(localPath); err != nil {
						localPath = "./artifacts/packages/component_" + strconv.Itoa(int(component.ID)) + "/" + f
					}
				}

					remotePath := component.DeployDir + "/" + remoteFilename

					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 上传安装包: " + remoteFilename + " -> " + remotePath + "\n"

					if _, err := os.Stat(localPath); err == nil {
						err := service.SSHSvc.UploadFile(&server, localPath, remotePath)
						if err != nil {
							allUploaded = false
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 上传失败 - " + err.Error() + "\n"
						} else {
							logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 上传成功\n"
						}
					} else {
						logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 跳过: 本地文件不存在 - " + localPath + "\n"
						allUploaded = false
					}
				}
				if hasFiles && !allUploaded {
					serverOK = false
					serverErr = "部分或全部安装包上传失败"
				}
			} else {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 无安装包需要上传\n"
			}
		}

		// 3. 执行安装命令（不阻塞部署成功判定，只记录）
		if serverOK && component.InstallCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.InstallCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 执行安装命令: " + component.InstallCmd + "\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 300*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		// 4. 启动服务（不阻塞部署成功判定，只记录）
		if serverOK && component.StartCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.StartCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 启动服务: " + component.StartCmd + "\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 60*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		// 5. 检查服务状态（不阻塞部署成功判定，只记录）
		if serverOK && component.StatusCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.StatusCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 检查服务状态...\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		if serverOK {
			succeededIDs = append(succeededIDs, server.ID)
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 服务器 " + server.Name + " 部署完成 ✓\n"
		} else {
			failedCount++
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 服务器 " + server.Name + " 部署失败 ✗ (" + serverErr + ")\n"
		}
	}

	logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 部署汇总 ======\n"
	logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 成功: " + strconv.Itoa(len(succeededIDs)) + "/" + strconv.Itoa(len(servers)) + " 台\n"
	if failedCount > 0 {
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 失败: " + strconv.Itoa(failedCount) + " 台\n"
	}

	// 决定组件状态：所有都成功 -> deployed；部分成功 -> partial；全失败 -> failed
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

	// DeployedServers 不在这里覆盖：保持跟用户配置的 server_ids 一致
	// 部署结果通过 status（deployed/partial/failed）和 deploy_log 体现
	// 这样编辑→部署→编辑 流程中,DeployedServers 不会被部署结果意外改掉

	logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 部署流程结束 ======\n"

	// 追加到现有日志,然后裁剪(保留最近 5 次 + 1MB 上限)
	if component.DeployLog != "" {
		component.DeployLog = truncateDeployLog(logContent + "\n" + component.DeployLog)
	} else {
		component.DeployLog = truncateDeployLog(logContent)
	}

	// 显式 Updates(map) 模式列出所有要更新的字段,避免 db.Save 在某些 GORM 版本下
	// 误把非零字段当成零值而漏写
	if err := database.DB.Model(&model.ProjectComponent{}).Where("id = ?", component.ID).Updates(map[string]interface{}{
		"status":           component.Status,
		"deployed_servers": component.DeployedServers,
		"deploy_log":       component.DeployLog,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deployment completed", "component": component})
}

type FetchVersionRequest struct {
	ServerIDs any `json:"server_ids"`
}

func (h *ProjectHandler) FetchVersion(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	if component.VersionCmd == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未配置获取版本命令（VersionCmd）"})
		return
	}

	var req FetchVersionRequest
	_ = c.ShouldBindJSON(&req) // body 可选

	// 决定目标服务器：请求中的 server_ids > DeployedServers > server_ids
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
	if len(serverIDs) == 0 && component.DeployedServers != "" {
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用的目标服务器，请先选择或部署"})
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

	// 在每台服务器上跑 VersionCmd
	// 用 service.SSHSvc.RunCommand 复用 SSH 连接缓存：同台服务器在多组件并发刷新时
	// 只建一次连接，而不是每个组件都重连。
	type serverResult struct {
		ServerID uint   `json:"server_id"`
		ServerIP string `json:"server_ip"`
		Output   string `json:"output"`
		Version  string `json:"version"` // 解析后的版本（首行非空）
		Err      string `json:"error,omitempty"`
	}
	results := make([]serverResult, 0, len(servers))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, s := range servers {
		wg.Add(1)
		go func(srv model.Server) {
			defer wg.Done()
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[PANIC FetchVersion server=%s] %v", srv.Name, err)
				}
			}()

			execCmd := component.VersionCmd
			if component.DeployDir != "" {
				execCmd = "cd " + component.DeployDir + " && " + component.VersionCmd
			}
			result, err := service.SSHSvc.RunCommand(&srv, execCmd, 30*time.Second)

			out := ""
			if result != nil {
				out = strings.TrimSpace(result.Output)
			}

			// RunCommand 出错时通常表示连接已失效；保留 output 方便排错
			if err != nil {
				mu.Lock()
				results = append(results, serverResult{ServerID: srv.ID, ServerIP: srv.IP, Output: out, Err: err.Error()})
				mu.Unlock()
				return
			}
			// exit code 非 0 也算失败
			if result != nil && result.ExitCode != 0 {
				msg := fmt.Sprintf("exit code %d", result.ExitCode)
				if result.Error != "" {
					msg = result.Error
				}
				mu.Lock()
				results = append(results, serverResult{ServerID: srv.ID, ServerIP: srv.IP, Output: out, Err: msg})
				mu.Unlock()
				return
			}

			// 解析版本号：首行非空内容
			ver := ""
			if out != "" {
				firstLine := strings.SplitN(out, "\n", 2)[0]
				ver = strings.TrimSpace(firstLine)
			}
			mu.Lock()
			results = append(results, serverResult{ServerID: srv.ID, ServerIP: srv.IP, Output: out, Version: ver})
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	// 整理结果：每台 server 自己的版本、主版本(取第一台成功的)
	version := ""
	successCount := 0
	var allOutput strings.Builder
	versionsMap := make(map[string]string, len(results))
	for _, r := range results {
		if r.Err == "" {
			successCount++
			if version == "" && r.Version != "" {
				version = r.Version
			}
			if r.Version != "" {
				versionsMap[strconv.Itoa(int(r.ServerID))] = r.Version
			}
		}
		allOutput.WriteString(fmt.Sprintf("[%s] 版本: %s\n", r.ServerIP, r.Version))
		if r.Output != "" && r.Output != r.Version {
			allOutput.WriteString(fmt.Sprintf("    完整输出: %s\n", r.Output))
		}
		if r.Err != "" {
			allOutput.WriteString(fmt.Sprintf("[%s] 错误: %s\n", r.ServerIP, r.Err))
		}
	}

	// 序列化每台 server 的版本到 JSON
	versionsJSON, _ := json.Marshal(versionsMap)

	// 版本是从命令获取的——获取多少就是多少，直接写回 DB
	if version != "" {
		if err := database.DB.Model(&model.ProjectComponent{}).Where("id = ?", component.ID).Updates(map[string]interface{}{
			"version":             version,
			"versions_per_server": string(versionsJSON),
		}).Error; err != nil {
			log.Printf("[FetchVersion] update version failed: %v", err)
		} else {
			component.Version = version
			component.VersionsPerServer = string(versionsJSON)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"version":       version,
		"output":        allOutput.String(),
		"results":       results,
		"success_count": successCount,
		"server_count":  len(servers),
		"component":     component,
	})
}

func (h *ProjectHandler) GetDeployLog(c *gin.Context) {
	id := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deploy_log": component.DeployLog})
}

func (h *ProjectHandler) DeletePackage(c *gin.Context) {
	componentID := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	if component.InstallPkg == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No package to delete"})
		return
	}

	targetFile := c.Query("file")

	if targetFile != "" {
		// 仅从列表中移除引用，不删原文件
		files := strings.Split(component.InstallPkg, ",")
		var remaining []string
		originalFile := c.Query("file")
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f != "" && f != originalFile {
				remaining = append(remaining, f)
			}
		}
		component.InstallPkg = strings.Join(remaining, ",")
	} else {
		component.InstallPkg = ""
	}

	database.DB.Save(&component)

	c.JSON(http.StatusOK, gin.H{"message": "Package deleted"})
}

func (h *ProjectHandler) CopyPackageFile(c *gin.Context) {
	componentID := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var project model.Project
	if err := database.DB.First(&project, component.ProjectID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Project not found"})
		return
	}

	var req struct {
		FilePath string `json:"file_path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	srcPath := strings.TrimPrefix(req.FilePath, "/")
	srcFull := filepath.Join("./artifacts", srcPath)
	srcFull = filepath.Clean(srcFull)

	if _, err := os.Stat(srcFull); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Source file not found"})
		return
	}

	filename := filepath.Base(srcFull)
	componentDir := projectPackageDir(&project, &component)
	dstPath := filepath.Join(componentDir, filename)

	if err := os.MkdirAll(componentDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	srcData, err := os.ReadFile(srcFull)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read source file"})
		return
	}

	if err := os.WriteFile(dstPath, srcData, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to copy file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "File copied successfully", "filename": filename})
}

func (h *ProjectHandler) UploadPackage(c *gin.Context) {
	componentID := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	var project model.Project
	if err := database.DB.First(&project, component.ProjectID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Project not found"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// 清理文件名：去掉路径分隔符和上级目录引用，防止路径穿越
	rawName := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	filename := sanitizeFilename(rawName)
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	componentDir := projectPackageDir(&project, &component)
	uploadPath := componentDir + "/" + filename

	if err := os.MkdirAll(componentDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	// 如果文件已存在，删除旧文件
	if _, err := os.Stat(uploadPath); err == nil {
		os.Remove(uploadPath)
	}

	dst, err := os.Create(uploadPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create file"})
		return
	}

	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(uploadPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	dst.Close()

	// 追加到组件的安装包列表，避免重复
	newPkg := filename
	if component.InstallPkg != "" {
		existing := strings.Split(component.InstallPkg, ",")
		dup := false
		for _, p := range existing {
			if strings.TrimSpace(p) == filename {
				dup = true
				break
			}
		}
		if dup {
			c.JSON(http.StatusOK, gin.H{"filename": filename, "path": uploadPath, "duplicate": true})
			return
		}
		newPkg = component.InstallPkg + "," + filename
	}
	component.InstallPkg = newPkg
	database.DB.Save(&component)

	c.JSON(http.StatusOK, gin.H{"filename": filename, "path": uploadPath})
}

// sanitizeFilename 去除文件名中的危险字符，只保留安全字符
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	invalid := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|", "\x00"}
	for _, ch := range invalid {
		name = strings.ReplaceAll(name, ch, "_")
	}
	name = strings.Trim(name, ". ")
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// projectPackageDir 生成项目对应的安装包目录：./artifacts/packages/<project_name>/
// 项目名做安全清理后作为顶层目录
func projectPackageDir(project *model.Project, component *model.ProjectComponent) string {
	projectDir := sanitizeFilename(project.Name)
	if projectDir == "" {
		projectDir = fmt.Sprintf("project_%d", project.ID)
	}
	return fmt.Sprintf("./artifacts/packages/%s", projectDir)
}

// componentPackagePath 返回组件安装包目录下某文件的本地完整路径
func componentPackagePath(project *model.Project, component *model.ProjectComponent, filename string) string {
	return filepath.Join(projectPackageDir(project, component), filename)
}

func InitDefaultProject() {
	var count int64
	database.DB.Model(&model.Project{}).Count(&count)
	if count > 0 {
		return
	}

	project := model.Project{
		Name:        "示例项目",
		Description: "包含所有基础服务的示例部署项目",
		IsDefault:   true,
		Status:      "active",
	}

	if err := database.DB.Create(&project).Error; err != nil {
		log.Printf("Failed to create default project: %v", err)
		return
	}

	components := []struct {
		Name    string
		Type    string
		Version string
	}{
		{"PostgreSQL", "database", "15"},
		{"Doris", "database", "2.0"},
		{"Flink", "compute", "1.17"},
		{"Kafka", "messagequeue", "3.4"},
		{"Nacos", "registry", "2.2"},
		{"DolphinScheduler", "scheduler", "3.1"},
		{"IoTDB", "database", "1.0"},
		{"MongoDB", "database", "6.0"},
		{"SeaTunnel", "etl", "2.3"},
		{"Redis", "cache", "7.0"},
		{"MinIO", "storage", "2024"},
		{"App1", "application", "1.0"},
		{"App2", "application", "1.0"},
		{"App3", "application", "1.0"},
	}

	for _, c := range components {
		component := model.ProjectComponent{
			ProjectID: project.ID,
			Name:      c.Name,
			Type:      c.Type,
			Version:   c.Version,
			Status:    "not_deployed",
		}
		database.DB.Create(&component)
	}

	log.Printf("Default project created with %d components", len(components))
}
