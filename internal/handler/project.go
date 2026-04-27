package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type ProjectHandler struct{}

func (h *ProjectHandler) ListProjects(c *gin.Context) {
	var projects []model.Project
	if err := database.DB.Preload("Components").Order("created_at DESC").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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

	if req.Name != "" {
		component.Name = req.Name
	}
	if req.Type != "" {
		component.Type = req.Type
	}
	if req.Description != "" {
		component.Description = req.Description
	}
	if req.Version != "" {
		component.Version = req.Version
	}
	if req.DeployDir != "" {
		component.DeployDir = req.DeployDir
	}
	if req.StatusCmd != "" {
		component.StatusCmd = req.StatusCmd
	}
	if req.LogCmd != "" {
		component.LogCmd = req.LogCmd
	}
	if req.AccessUser != "" {
		component.AccessUser = req.AccessUser
	}
	if req.AccessPassword != "" {
		component.AccessPassword = req.AccessPassword
	}
	if req.AccessURL != "" {
		component.AccessURL = req.AccessURL
	}
	if req.InstallCmd != "" {
		component.InstallCmd = req.InstallCmd
	}
	if req.StartCmd != "" {
		component.StartCmd = req.StartCmd
	}
	if req.StopCmd != "" {
		component.StopCmd = req.StopCmd
	}
	if req.ConfigFile != "" {
		component.ConfigFile = req.ConfigFile
	}
	if req.Status != "" {
		component.Status = req.Status
	}
	if req.ServerIDs != nil {
		data, _ := json.Marshal(req.ServerIDs)
		component.ServerIDs = string(data)
	}

	if err := database.DB.Save(&component).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"component": component})
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

	// 存储所有部署的服务器ID
	component.Status = "deployed"
	component.DeployedServers = strconv.Itoa(int(serverIDs[0]))
	if len(serverIDs) > 1 {
		for i := 1; i < len(serverIDs); i++ {
			component.DeployedServers += "," + strconv.Itoa(int(serverIDs[i]))
		}
	}

	logTime := time.Now().Format("2006-01-02 15:04:05")
	var logContent string
	logContent = "[" + logTime + "] ====== 开始部署 ======\n"
	logContent += "[" + logTime + "] 组件: " + component.Name + "\n"
	logContent += "[" + logTime + "] 类型: " + component.Type + "\n"
	logContent += "[" + logTime + "] 版本: " + component.Version + "\n"

	// 部署到所有服务器
	for idx, server := range servers {
		logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 服务器 " + strconv.Itoa(idx+1) + "/" + strconv.Itoa(len(servers)) + ": " + server.Name + " (" + server.IP + ") ======\n"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 连接: " + server.IP + ":" + strconv.Itoa(server.Port) + "\n"
		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 部署目录: " + component.DeployDir + "\n"

		// 1. 创建部署目录
		if component.DeployDir != "" {
			err := service.SSHSvc.RemoteMkdir(&server, component.DeployDir)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 创建目录失败 - " + err.Error() + "\n"
			} else {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 目录创建成功\n"
			}
		}

		// 2. 上传安装包
		if component.InstallPkg != "" {
			files := strings.Split(component.InstallPkg, ",")
			for _, f := range files {
				f = strings.TrimSpace(f)
				if f == "" {
					continue
				}
				localPath := "./artifacts/packages/component_" + strconv.Itoa(int(component.ID)) + "/" + f
				remotePath := component.DeployDir + "/" + f

				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 上传安装包: " + f + " -> " + remotePath + "\n"

				if _, err := os.Stat(localPath); err == nil {
					err := service.SSHSvc.UploadFile(&server, localPath, remotePath)
					if err != nil {
						logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: 上传失败 - " + err.Error() + "\n"
					} else {
						logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 上传成功\n"
					}
				} else {
					logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 跳过: 本地文件不存在 - " + localPath + "\n"
				}
			}
		} else {
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 无安装包需要上传\n"
		}

		// 3. 执行安装命令
		if component.InstallCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.InstallCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 执行安装命令: " + component.InstallCmd + "\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 300*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		// 4. 启动服务
		if component.StartCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.StartCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 启动服务: " + component.StartCmd + "\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 60*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		// 5. 检查服务状态
		if component.StatusCmd != "" {
			cmd := "cd " + component.DeployDir + " && " + component.StatusCmd
			logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 检查服务状态...\n"
			output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
			if err != nil {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 错误: " + err.Error() + "\n"
			} else if output != "" {
				logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] " + output + "\n"
			}
		}

		logContent += "[" + time.Now().Format("2006-01-02 15:04:05") + "] 服务器 " + server.Name + " 部署完成\n"
	}

	logContent += "\n[" + time.Now().Format("2006-01-02 15:04:05") + "] ====== 全部部署完成 ======\n"

	// 追加到现有日志
	if component.DeployLog != "" {
		component.DeployLog = logContent + "\n" + component.DeployLog
	} else {
		component.DeployLog = logContent
	}

	if err := database.DB.Save(&component).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deployment completed", "component": component})
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
		// 删除指定文件
		uploadPath := "./artifacts/packages/" + targetFile
		if err := os.Remove(uploadPath); err != nil {
			log.Printf("Failed to delete package file %s: %v", targetFile, err)
		}

		// 从列表中移除
		files := strings.Split(component.InstallPkg, ",")
		var remaining []string
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f != "" && f != targetFile {
				remaining = append(remaining, f)
			}
		}
		component.InstallPkg = strings.Join(remaining, ",")
	} else {
		// 删除所有文件
		files := strings.Split(component.InstallPkg, ",")
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			uploadPath := fmt.Sprintf("./artifacts/packages/component_%d/%s", component.ID, f)
			if err := os.Remove(uploadPath); err != nil {
				log.Printf("Failed to delete package file %s: %v", f, err)
			}
		}
		component.InstallPkg = ""
	}

	database.DB.Save(&component)

	c.JSON(http.StatusOK, gin.H{"message": "Package deleted"})
}

func (h *ProjectHandler) UploadPackage(c *gin.Context) {
	componentID := c.Param("id")
	var component model.ProjectComponent
	if err := database.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// 使用原始文件名，保存在组件对应的文件夹中
	filename := header.Filename
	componentDir := fmt.Sprintf("./artifacts/packages/component_%d", component.ID)
	uploadPath := componentDir + "/" + filename

	// 如果文件已存在，删除旧文件
	if _, err := os.Stat(uploadPath); err == nil {
		os.Remove(uploadPath)
	}

	if err := os.MkdirAll(componentDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	dst, err := os.Create(uploadPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create file"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}

	if component.InstallPkg != "" {
		component.InstallPkg += "," + filename
	} else {
		component.InstallPkg = filename
	}
	database.DB.Save(&component)

	c.JSON(http.StatusOK, gin.H{"filename": filename, "path": uploadPath})
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
