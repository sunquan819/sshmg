package handler

import (
	"context"
	"net/http"
	"strconv"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type ContainerHandler struct{}

func (h *ContainerHandler) ListContainers(c *gin.Context) {
	serverID := c.Query("server_id")
	allStr := c.Query("all")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	all := allStr == "true"
	containers, err := service.DockerSvc.ListContainers(&server, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

func (h *ContainerHandler) StartContainer(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	err = service.DockerSvc.StartContainer(&server, containerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "容器已启动"})
}

func (h *ContainerHandler) StopContainer(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	err = service.DockerSvc.StopContainer(&server, containerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "容器已停止"})
}

func (h *ContainerHandler) RestartContainer(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	err = service.DockerSvc.RestartContainer(&server, containerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "容器已重启"})
}

func (h *ContainerHandler) RemoveContainer(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")
	force := c.Query("force") == "true"

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	err = service.DockerSvc.RemoveContainer(context.Background(), &server, containerID, force)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "容器已删除"})
}

func (h *ContainerHandler) GetContainerLogs(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")
	tail := c.DefaultQuery("tail", "200")
	offset := c.DefaultQuery("offset", "0")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	logs, err := service.DockerSvc.GetContainerLogs(context.Background(), &server, containerID, tail, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

func (h *ContainerHandler) GetContainerStats(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	stats, err := service.DockerSvc.GetContainerStats(&server, containerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

func (h *ContainerHandler) ExecContainer(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	var req struct {
		Command string `json:"command" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	output, err := service.DockerSvc.ExecContainer(&server, containerID, req.Command)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"output": output})
}

func (h *ContainerHandler) GetContainerInfo(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	info, err := service.DockerSvc.GetContainerInfo(&server, containerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

func (h *ContainerHandler) ListImages(c *gin.Context) {
	serverID := c.Query("server_id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	images, err := service.DockerSvc.ListImages(&server)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"images": images})
}

func (h *ContainerHandler) CheckDocker(c *gin.Context) {
	serverID := c.Query("server_id")

	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	installed := service.DockerSvc.CheckDockerInstalled(&server)
	c.JSON(http.StatusOK, gin.H{"installed": installed, "ip": server.IP})
}
