package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type TemplateHandler struct{}

func NewTemplateHandler() *TemplateHandler {
	return &TemplateHandler{}
}

type ComposeTemplate struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at"`
}

type TemplateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Content     string `json:"content" binding:"required"`
}

func getAppDirForHandler() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	var templates []model.ComposeTemplate
	if err := database.DB.Order("created_at desc").Find(&templates).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"templates": []model.ComposeTemplate{}})
		return
	}

	if len(templates) == 0 {
		var defaultTemplates []ComposeTemplate
		externalTemplatesPath := filepath.Join(getAppDirForHandler(), "templates", "compose.json")
		if data, err := os.ReadFile(externalTemplatesPath); err == nil {
			json.Unmarshal(data, &defaultTemplates)
		}

		for _, t := range defaultTemplates {
			template := model.ComposeTemplate{
				Name:        t.Name,
				Description: t.Description,
				Content:     t.Content,
			}
			database.DB.Create(&template)
		}

		database.DB.Order("created_at desc").Find(&templates)
	}

	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template id"})
		return
	}

	var template model.ComposeTemplate
	if err := database.DB.First(&template, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	c.JSON(http.StatusOK, template)
}

func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	template := model.ComposeTemplate{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
	}

	if err := database.DB.Create(&template).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, template)
}

func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template id"})
		return
	}

	var template model.ComposeTemplate
	if err := database.DB.First(&template, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	template.Name = req.Name
	template.Description = req.Description
	template.Content = req.Content

	database.DB.Save(&template)
	c.JSON(http.StatusOK, template)
}

func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid template id"})
		return
	}

	if err := database.DB.Delete(&model.ComposeTemplate{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *TemplateHandler) ValidateContent(c *gin.Context) {
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	content := req.Content

	errors := []string{}

	matched := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*:`).MatchString(content)
	if !matched && len(content) > 0 {
		errors = append(errors, "YAML 格式可能不正确，服务名应以字母或下划线开头")
	}

	if len(content) < 10 {
		errors = append(errors, "内容过短")
	}

	if len(errors) == 0 {
		c.JSON(http.StatusOK, gin.H{"valid": true, "errors": []string{}})
	} else {
		c.JSON(http.StatusOK, gin.H{"valid": false, "errors": errors})
	}
}
