package handler

import (
	"net/http"
	"strconv"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type CronHandler struct{}

func NewCronHandler() *CronHandler {
	return &CronHandler{}
}

type CreateCronRequest struct {
	ServerID uint   `json:"server_id" binding:"required"`
	Name     string `json:"name"`
	Schedule string `json:"schedule" binding:"required"`
	Command  string `json:"command" binding:"required"`
}

type UpdateCronRequest struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Status   string `json:"status"`
}

func (h *CronHandler) ListCronJobs(c *gin.Context) {
	var cronJobs []model.CronJob
	query := database.DB

	serverID := c.Query("server_id")
	if serverID != "" {
		query = query.Where("server_id = ?", serverID)
	}

	status := c.Query("status")
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Order("created_at DESC").Find(&cronJobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cron_jobs": cronJobs})
}

func (h *CronHandler) GetCronJob(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job id"})
		return
	}

	var cronJob model.CronJob
	if err := database.DB.First(&cronJob, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
		return
	}

	c.JSON(http.StatusOK, cronJob)
}

func (h *CronHandler) CreateCronJob(c *gin.Context) {
	var req CreateCronRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, req.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	cronJob := model.CronJob{
		ServerID: req.ServerID,
		Name:     req.Name,
		Schedule: req.Schedule,
		Command:  req.Command,
		Status:   "active",
	}

	if err := database.DB.Create(&cronJob).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := service.CronSvc.AddCronJob(&server, req.Name, req.Schedule, req.Command); err != nil {
		database.DB.Delete(&cronJob)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add cron job: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, cronJob)
}

func (h *CronHandler) UpdateCronJob(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job id"})
		return
	}

	var req UpdateCronRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var cronJob model.CronJob
	if err := database.DB.First(&cronJob, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Schedule != "" {
		updates["schedule"] = req.Schedule
	}
	if req.Command != "" {
		updates["command"] = req.Command
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}

	if err := database.DB.Model(&cronJob).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, cronJob)
}

func (h *CronHandler) DeleteCronJob(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job id"})
		return
	}

	var cronJob model.CronJob
	if err := database.DB.First(&cronJob, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, cronJob.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	if err := service.CronSvc.RemoveCronJob(&server, cronJob.Schedule, cronJob.Command); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Delete(&cronJob).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "cron job deleted successfully"})
}

func (h *CronHandler) ExecuteCronJob(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job id"})
		return
	}

	var cronJob model.CronJob
	if err := database.DB.First(&cronJob, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, cronJob.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	start := time.Now()
	output, err := service.CronSvc.ExecuteCommand(&server, cronJob.Command, 300*time.Second)
	duration := time.Since(start)

	history := model.CronHistory{
		CronJobID:  cronJob.ID,
		ServerID:   cronJob.ServerID,
		Command:    cronJob.Command,
		Output:     output,
		ExitCode:   0,
		Duration:   duration.Milliseconds(),
		ExecutedAt: start,
	}

	if err != nil {
		history.Error = err.Error()
		history.ExitCode = 1
	}

	database.DB.Create(&history)

	now := time.Now()
	database.DB.Model(&cronJob).Updates(map[string]interface{}{
		"last_run": &now,
	})

	c.JSON(http.StatusOK, gin.H{
		"output":   output,
		"error":    err,
		"duration": duration.String(),
	})
}

func (h *CronHandler) ListCronHistory(c *gin.Context) {
	var history []model.CronHistory
	query := database.DB

	cronJobID := c.Query("cron_job_id")
	if cronJobID != "" {
		query = query.Where("cron_job_id = ?", cronJobID)
	}

	serverID := c.Query("server_id")
	if serverID != "" {
		query = query.Where("server_id = ?", serverID)
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if err := query.Order("executed_at DESC").Limit(limit).Find(&history).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"history": history})
}

func (h *CronHandler) ListServerCronJobs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	jobs, err := service.CronSvc.ListCronJobs(&server)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}
