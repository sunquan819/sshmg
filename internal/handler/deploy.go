package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type DeployHandler struct{}

func NewDeployHandler() *DeployHandler {
	return &DeployHandler{}
}

type DeployRequest struct {
	ServerID uint            `json:"server_id" binding:"required"`
	Type     string          `json:"type" binding:"required"`
	Name     string          `json:"name" binding:"required"`
	Config   json.RawMessage `json:"config" binding:"required"`
}

type ProcessDeployConfig struct {
	Command   string   `json:"command"`
	WorkDir   string   `json:"work_dir"`
	User      string   `json:"user"`
	Env       []string `json:"env"`
	Restart   string   `json:"restart"`
	StdoutLog string   `json:"stdout_log"`
	StderrLog string   `json:"stderr_log"`
}

type ContainerDeployConfig struct {
	Image   string            `json:"image"`
	Cmd     []string          `json:"cmd"`
	Env     []string          `json:"env"`
	Ports   map[string]string `json:"ports"`
	Volumes map[string]string `json:"volumes"`
	Restart string            `json:"restart"`
}

type ComposeDeployConfig struct {
	Content string `json:"content"`
}

func (h *DeployHandler) ListDeployments(c *gin.Context) {
	var deployments []model.Deployment
	query := database.DB

	serverID := c.Query("server_id")
	if serverID != "" {
		query = query.Where("server_id = ?", serverID)
	}

	deployType := c.Query("type")
	if deployType != "" {
		query = query.Where("type = ?", deployType)
	}

	status := c.Query("status")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Preload("Server").Order("created_at DESC").Find(&deployments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deployments": deployments})
}

func (h *DeployHandler) GetDeployment(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	var deployment model.Deployment
	if err := database.DB.Preload("Server").First(&deployment, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	c.JSON(http.StatusOK, deployment)
}

func (h *DeployHandler) CreateDeployment(c *gin.Context) {
	var req DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, req.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	deployment := model.Deployment{
		ServerID: req.ServerID,
		Type:     req.Type,
		Name:     req.Name,
		Config:   string(req.Config),
		Status:   "pending",
	}

	if err := database.DB.Create(&deployment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	go func() {
		var dep model.Deployment
		database.DB.First(&dep, deployment.ID)
		var srv model.Server
		database.DB.First(&srv, server.ID)
		h.executeDeployment(&dep, &srv)
	}()

	c.JSON(http.StatusCreated, deployment)
}

func (h *DeployHandler) executeDeployment(deployment *model.Deployment, server *model.Server) {
	fmt.Printf("Starting deployment %d, type: %s\n", deployment.ID, deployment.Type)

	database.DB.Model(deployment).Updates(map[string]interface{}{
		"status": "running",
	})

	var errMsg string
	var err error

	switch deployment.Type {
	case "process":
		err = h.deployProcess(server, deployment)
	case "container":
		err = h.deployContainer(server, deployment)
	case "compose":
		err = h.deployCompose(server, deployment)
	default:
		errMsg = "unknown deployment type"
	}

	if err != nil {
		errMsg = err.Error()
		fmt.Printf("Deployment %d failed: %s\n", deployment.ID, errMsg)
		database.DB.Model(deployment).Updates(map[string]interface{}{
			"status":  "failed",
			"message": errMsg,
		})
	} else {
		fmt.Printf("Deployment %d succeeded\n", deployment.ID)
		database.DB.Model(deployment).Updates(map[string]interface{}{
			"status":  "success",
			"message": "deployment completed successfully",
		})
	}
}

func (h *DeployHandler) deployProcess(server *model.Server, deployment *model.Deployment) error {
	var cfg ProcessDeployConfig
	if err := json.Unmarshal([]byte(deployment.Config), &cfg); err != nil {
		return err
	}

	processCfg := &service.ProcessConfig{
		Name:      deployment.Name,
		Command:   cfg.Command,
		WorkDir:   cfg.WorkDir,
		User:      cfg.User,
		Env:       cfg.Env,
		Restart:   cfg.Restart,
		StdoutLog: cfg.StdoutLog,
		StderrLog: cfg.StderrLog,
	}

	if err := service.ProcessSvc.CreateSystemdService(server, processCfg); err != nil {
		return err
	}

	if err := service.ProcessSvc.EnableService(server, deployment.Name); err != nil {
		return err
	}

	return service.ProcessSvc.StartService(server, deployment.Name)
}

func (h *DeployHandler) deployContainer(server *model.Server, deployment *model.Deployment) error {
	var cfg ContainerDeployConfig
	if err := json.Unmarshal([]byte(deployment.Config), &cfg); err != nil {
		return err
	}

	cmd := fmt.Sprintf("docker run -d --name %s", deployment.Name)

	if cfg.Restart == "always" {
		cmd += " --restart always"
	}

	for hostPort, containerPort := range cfg.Ports {
		cmd += fmt.Sprintf(" -p %s:%s", hostPort, containerPort)
	}

	for _, env := range cfg.Env {
		cmd += fmt.Sprintf(" -e '%s'", env)
	}

	cmd += fmt.Sprintf(" %s", cfg.Image)

	_, err := service.SSHSvc.ExecuteCommand(server, cmd, 300*time.Second)
	return err
}

func (h *DeployHandler) deployCompose(server *model.Server, deployment *model.Deployment) error {
	var cfg ComposeDeployConfig
	if err := json.Unmarshal([]byte(deployment.Config), &cfg); err != nil {
		return err
	}

	dockerCheck, _ := service.SSHSvc.ExecuteCommand(server, "docker info 2>&1 | head -5", 10*time.Second)
	if strings.Contains(dockerCheck, "Cannot connect") || strings.Contains(dockerCheck, "daemon not running") {
		return fmt.Errorf("Docker 守护进程未运行，请启动 Docker: %s", dockerCheck)
	}
	if strings.Contains(dockerCheck, "command not found") || dockerCheck == "" {
		return fmt.Errorf("Docker 未安装，请先安装 Docker")
	}

	homeOutput, err := service.SSHSvc.ExecuteCommand(server, "echo $HOME", 5*time.Second)
	if err != nil || homeOutput == "" {
		homeOutput = "/root"
	}
	homeOutput = strings.TrimSpace(homeOutput)
	composeDir := fmt.Sprintf("%s/compose_%d", homeOutput, deployment.ID)

	_, err = service.SSHSvc.ExecuteCommand(server, "mkdir -p "+composeDir, 10*time.Second)
	if err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	composeFilePath := composeDir + "/docker-compose.yml"
	err = service.FileSvc.WriteFile(server, composeFilePath, cfg.Content)
	if err != nil {
		return fmt.Errorf("写入 compose 文件失败: %w", err)
	}

	cmd := fmt.Sprintf("cd %s && docker-compose config 2>&1", composeDir)
	configCheck, _ := service.SSHSvc.ExecuteCommand(server, cmd, 30*time.Second)
	fmt.Printf("Deployment %d: docker-compose config:\n%s\n", deployment.ID, configCheck)

	cmd = fmt.Sprintf("cd %s && docker-compose down 2>&1", composeDir)
	service.SSHSvc.ExecuteCommand(server, cmd, 60*time.Second)

	cmd = fmt.Sprintf("cd %s && docker-compose up -d 2>&1", composeDir)
	output, err := service.SSHSvc.ExecuteCommand(server, cmd, 300*time.Second)
	fmt.Printf("Deployment %d: docker-compose up output: %s, error: %v\n", deployment.ID, output, err)

	listOutput, _ := service.SSHSvc.ExecuteCommand(server, "docker ps -a --format '{{.Names}}:{{.Status}}'", 10*time.Second)
	fmt.Printf("Deployment %d 容器列表:\n%s\n", deployment.ID, listOutput)

	resultMsg := fmt.Sprintf("部署目录: %s\n执行输出: %s\n容器状态:\n%s", composeDir, output, listOutput)
	database.DB.Model(deployment).Update("message", resultMsg)

	hasError := err != nil || strings.Contains(output, "Error") || strings.Contains(output, "error") || strings.Contains(output, "failed")
	if hasError {
		return fmt.Errorf(resultMsg)
	}

	return nil
}

func (h *DeployHandler) DeleteDeployment(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	var deployment model.Deployment
	if err := database.DB.First(&deployment, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, deployment.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	switch deployment.Type {
	case "process":
		if err := service.ProcessSvc.RemoveService(&server, deployment.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "container":
		cmd := fmt.Sprintf("docker rm -f %s", deployment.Name)
		if _, err := service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if err := database.DB.Delete(&deployment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deployment deleted successfully"})
}

func (h *DeployHandler) RestartDeployment(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	var deployment model.Deployment
	if err := database.DB.First(&deployment, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, deployment.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	switch deployment.Type {
	case "process":
		if err := service.ProcessSvc.RestartService(&server, deployment.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "container":
		cmd := fmt.Sprintf("docker restart %s", deployment.Name)
		if _, err := service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "deployment restarted successfully"})
}

func (h *DeployHandler) GetDeploymentLogs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	var deployment model.Deployment
	if err := database.DB.First(&deployment, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, deployment.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	var logs string
	switch deployment.Type {
	case "process":
		logs, err = service.ProcessSvc.GetServiceLogs(&server, deployment.Name, 100)
	case "container":
		cmd := fmt.Sprintf("docker logs --tail 100 %s", deployment.Name)
		logs, err = service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
	default:
		err = fmt.Errorf("unknown deployment type")
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}
