package handler

import (
	"net/http"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type CommandHandler struct{}

func NewCommandHandler() *CommandHandler {
	return &CommandHandler{}
}

type CommandRequest struct {
	Name     string `json:"name" binding:"required"`
	Command  string `json:"command" binding:"required"`
	Category string `json:"category"`
}

func (h *CommandHandler) InitDefaultCommands() {
	var count int64
	database.DB.Model(&model.Command{}).Count(&count)
	if count > 0 {
		return
	}

	for _, cmd := range model.DefaultCommands {
		cmd.IsDefault = true
		if cmd.Category == "" {
			cmd.Category = "系统"
		}
		database.DB.Create(&cmd)
	}
}

func (h *CommandHandler) ListCommands(c *gin.Context) {
	var commands []model.Command
	query := database.DB.Order("category, name")

	category := c.Query("category")
	if category != "" {
		query = query.Where("category = ?", category)
	}

	if err := query.Find(&commands).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"commands": commands})
}

func (h *CommandHandler) CreateCommand(c *gin.Context) {
	var req CommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cmd := model.Command{
		Name:     req.Name,
		Command:  req.Command,
		Category: req.Category,
		IsDefault: false,
	}

	if cmd.Category == "" {
		cmd.Category = "自定义"
	}

	if err := database.DB.Create(&cmd).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"command": cmd})
}

func (h *CommandHandler) UpdateCommand(c *gin.Context) {
	id := c.Param("id")

	var existing model.Command
	if err := database.DB.First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "command not found"})
		return
	}

	if existing.IsDefault {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot modify default command"})
		return
	}

	var req CommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing.Name = req.Name
	existing.Command = req.Command
	existing.Category = req.Category

	if err := database.DB.Save(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"command": existing})
}

func (h *CommandHandler) DeleteCommand(c *gin.Context) {
	id := c.Param("id")

	var cmd model.Command
	if err := database.DB.First(&cmd, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "command not found"})
		return
	}

	if cmd.IsDefault {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete default command"})
		return
	}

	if err := database.DB.Delete(&cmd).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
