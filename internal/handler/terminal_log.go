package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type TerminalLogHandler struct{}

func NewTerminalLogHandler() *TerminalLogHandler {
	return &TerminalLogHandler{}
}

type TerminalLogRequest struct {
	ServerID    uint   `json:"server_id"`
	ServerName  string `json:"server_name"`
	ServerIP    string `json:"server_ip"`
	SystemUser  string `json:"system_user"`
	SessionType string `json:"session_type"`
	StartTime   string `json:"start_time"`
	Commands    string `json:"commands"`
}

func (h *TerminalLogHandler) ListTerminalLogs(c *gin.Context) {
	var logs []model.TerminalSessionLog
	query := database.DB.Order("created_at desc")

	search := c.Query("search")
	if search != "" {
		query = query.Where("server_name LIKE ? OR server_ip LIKE ? OR system_user LIKE ? OR commands LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	serverID := c.Query("server_id")
	if serverID != "" {
		query = query.Where("server_id = ?", serverID)
	}

	sessionType := c.Query("session_type")
	if sessionType != "" {
		query = query.Where("session_type = ?", sessionType)
	}

	sort := c.Query("sort")
	if sort == "asc" {
		query = database.DB.Order("created_at asc")
	} else if sort == "start_time" {
		query = database.DB.Order("start_time desc")
	}

	if err := query.Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": len(logs)})
}

func (h *TerminalLogHandler) GetTerminalLog(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log id"})
		return
	}

	var log model.TerminalSessionLog
	if err := database.DB.First(&log, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "log not found"})
		return
	}

	var commands []model.TerminalCommand
	if log.Commands != "" {
		if err := json.Unmarshal([]byte(log.Commands), &commands); err != nil {
			commands = []model.TerminalCommand{}
		}
	}

	c.JSON(http.StatusOK, gin.H{"log": log, "commands": commands})
}

func (h *TerminalLogHandler) CreateTerminalLog(c *gin.Context) {
	var req TerminalLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		startTime = time.Now()
	}

	log := model.TerminalSessionLog{
		ServerID:    req.ServerID,
		ServerName:  req.ServerName,
		ServerIP:    req.ServerIP,
		SystemUser:  req.SystemUser,
		SessionType: req.SessionType,
		StartTime:   startTime,
		EndTime:     time.Now(),
		Commands:    req.Commands,
	}

	if err := database.DB.Create(&log).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"log": log, "id": log.ID})
}

func (h *TerminalLogHandler) DeleteTerminalLog(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log id"})
		return
	}

	if err := database.DB.Delete(&model.TerminalSessionLog{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *TerminalLogHandler) DeleteTerminalLogs(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Delete(&model.TerminalSessionLog{}, req.IDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted", "count": len(req.IDs)})
}

func (h *TerminalLogHandler) ClearOldLogs(c *gin.Context) {
	days := c.Query("days")
	if days == "" {
		days = "30"
	}

	daysInt, err := strconv.Atoi(days)
	if err != nil {
		daysInt = 30
	}

	cutoff := time.Now().AddDate(0, 0, -daysInt)

	result := database.DB.Where("created_at < ?", cutoff).Delete(&model.TerminalSessionLog{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted", "count": result.RowsAffected})
}

func (h *TerminalLogHandler) GetServerStats(c *gin.Context) {
	var stats []struct {
		ServerID   uint
		ServerName string
		ServerIP   string
		Count      int
	}

	database.DB.Table("terminal_session_logs").
		Select("server_id, server_name, server_ip, count(*) as count").
		Group("server_id, server_name, server_ip").
		Order("count desc").
		Find(&stats)

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}