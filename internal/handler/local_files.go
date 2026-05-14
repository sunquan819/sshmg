package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type LocalFilesHandler struct {
	baseDir string
}

func NewLocalFilesHandler() *LocalFilesHandler {
	return &LocalFilesHandler{
		baseDir: "./artifacts",
	}
}

func (h *LocalFilesHandler) ListFiles(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		relativePath = "/"
	}

	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	files, err := h.listDirectory(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":  "/" + relativePath,
		"files": files,
	})
}

func (h *LocalFilesHandler) listDirectory(path string) ([]map[string]interface{}, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}

	var files []map[string]interface{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		relPath := strings.TrimPrefix(fullPath, h.baseDir)

		file := map[string]interface{}{
			"name":   entry.Name(),
			"path":   "/" + relPath,
			"is_dir": entry.IsDir(),
			"size":   info.Size(),
			"mod_time": info.ModTime().Format("2006-01-02 15:04:05"),
		}
		files = append(files, file)
	}
	return files, nil
}

func (h *LocalFilesHandler) CreateDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	relativePath := strings.TrimPrefix(req.Path, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "directory created successfully", "path": req.Path})
}

func (h *LocalFilesHandler) UploadFile(c *gin.Context) {
	relativePath := c.PostForm("path")
	if relativePath == "" {
		relativePath = "/"
	}
	relativePath = strings.TrimPrefix(relativePath, "/")

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	targetDir := filepath.Join(h.baseDir, relativePath)
	if !strings.HasPrefix(targetDir, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	targetPath := filepath.Join(targetDir, file.Filename)
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	relPath := strings.TrimPrefix(targetPath, h.baseDir)
	c.JSON(http.StatusOK, gin.H{
		"message":  "file uploaded successfully",
		"path":     "/" + relPath,
		"filename": file.Filename,
		"size":     file.Size,
	})
}

func (h *LocalFilesHandler) DownloadFile(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot download directory"})
		return
	}

	filename := filepath.Base(fullPath)
	encodedFilename := url.QueryEscape(filename)
	c.Header("Content-Disposition", "attachment; filename="+encodedFilename+"; filename*=UTF-8''"+encodedFilename)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.File(fullPath)
}

func (h *LocalFilesHandler) DeleteFile(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if fullPath == h.baseDir {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete base directory"})
		return
	}

	if err := os.RemoveAll(fullPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted successfully"})
}

func (h *LocalFilesHandler) RenameFile(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	relativePath := strings.TrimPrefix(req.Path, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	dir := filepath.Dir(fullPath)
	newPath := filepath.Join(dir, req.Name)

	if !strings.HasPrefix(newPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
		return
	}

	if err := os.Rename(fullPath, newPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	relPath := strings.TrimPrefix(newPath, h.baseDir)
	c.JSON(http.StatusOK, gin.H{
		"message": "renamed successfully",
		"path":    "/" + relPath,
	})
}

func (h *LocalFilesHandler) MoveFile(c *gin.Context) {
	var req struct {
		Src string `json:"src" binding:"required"`
		Dst string `json:"dst" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	srcPath := strings.TrimPrefix(req.Src, "/")
	dstPath := strings.TrimPrefix(req.Dst, "/")

	fullSrc := filepath.Join(h.baseDir, srcPath)
	fullDst := filepath.Join(h.baseDir, dstPath)

	if !strings.HasPrefix(fullSrc, h.baseDir) || !strings.HasPrefix(fullDst, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if err := os.Rename(fullSrc, fullDst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "moved successfully"})
}

func (h *LocalFilesHandler) CopyFile(c *gin.Context) {
	var req struct {
		Src string `json:"src" binding:"required"`
		Dst string `json:"dst" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	srcPath := strings.TrimPrefix(req.Src, "/")
	dstPath := strings.TrimPrefix(req.Dst, "/")

	fullSrc := filepath.Join(h.baseDir, srcPath)
	fullDst := filepath.Join(h.baseDir, dstPath)

	if !strings.HasPrefix(fullSrc, h.baseDir) || !strings.HasPrefix(fullDst, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	srcInfo, err := os.Stat(fullSrc)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source not found"})
		return
	}

	if srcInfo.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot copy directory"})
		return
	}

	srcFile, err := os.Open(fullSrc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(fullDst), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dstFile, err := os.Create(fullDst)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "copied successfully"})
}

func (h *LocalFilesHandler) ReadFile(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read directory"})
		return
	}

	if info.Size() > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 10MB)"})
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":    "/" + relativePath,
		"content": string(content),
		"size":    info.Size(),
	})
}

func (h *LocalFilesHandler) WriteFile(c *gin.Context) {
	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	relativePath := strings.TrimPrefix(req.Path, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "file written successfully"})
}

func (h *LocalFilesHandler) GetFileInfo(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	relativePath = strings.TrimPrefix(relativePath, "/")
	fullPath := filepath.Join(h.baseDir, relativePath)

	if !strings.HasPrefix(fullPath, h.baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":     "/" + relativePath,
		"name":     info.Name(),
		"is_dir":   info.IsDir(),
		"size":     info.Size(),
		"mod_time": info.ModTime().Format("2006-01-02 15:04:05"),
	})
}

func RegisterLocalFilesRoutes(r *gin.RouterGroup, handler *LocalFilesHandler) {
	files := r.Group("/local-files")
	{
		files.GET("", handler.ListFiles)
		files.GET("/list", handler.ListFiles)
		files.GET("/info", handler.GetFileInfo)
		files.GET("/read", handler.ReadFile)
		files.GET("/download", handler.DownloadFile)
		files.POST("/mkdir", handler.CreateDirectory)
		files.POST("/upload", handler.UploadFile)
		files.POST("/write", handler.WriteFile)
		files.POST("/rename", handler.RenameFile)
		files.POST("/move", handler.MoveFile)
		files.POST("/copy", handler.CopyFile)
		files.DELETE("", handler.DeleteFile)
	}
}

