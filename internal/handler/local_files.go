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
	baseDir  string
	absBase  string
}

func NewLocalFilesHandler() *LocalFilesHandler {
	absBase, err := filepath.Abs("./artifacts")
	if err != nil {
		absBase = "./artifacts"
	}
	return &LocalFilesHandler{
		baseDir: "./artifacts",
		absBase: absBase,
	}
}

func (h *LocalFilesHandler) checkPath(relativePath string) (string, error) {
	relativePath = strings.TrimPrefix(relativePath, "/")
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	
	fullPath := filepath.Join(h.absBase, relativePath)
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absFull, h.absBase) {
		return "", fmt.Errorf("invalid path")
	}

	return absFull, nil
}

func (h *LocalFilesHandler) toRelPath(absPath string) string {
	relPath := strings.TrimPrefix(absPath, h.absBase)
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	relPath = strings.TrimPrefix(relPath, "/")
	return "/" + relPath
}

func (h *LocalFilesHandler) ListFiles(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		relativePath = "/"
	}

	absPath, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	files, err := h.listDirectory(absPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":  "/" + strings.TrimPrefix(relativePath, "/"),
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
		relPath := h.toRelPath(fullPath)

		file := map[string]interface{}{
			"name":     entry.Name(),
			"path":     relPath,
			"is_dir":   entry.IsDir(),
			"size":     info.Size(),
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

	absPath, err := h.checkPath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
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

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	absDir, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	targetPath := filepath.Join(absDir, file.Filename)
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	relPath := h.toRelPath(targetPath)
	c.JSON(http.StatusOK, gin.H{
		"message":  "file uploaded successfully",
		"path":     relPath,
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

	absPath, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot download directory"})
		return
	}

	filename := filepath.Base(absPath)
	encodedFilename := url.QueryEscape(filename)
	c.Header("Content-Disposition", "attachment; filename="+encodedFilename+"; filename*=UTF-8''"+encodedFilename)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.File(absPath)
}

func (h *LocalFilesHandler) DeleteFile(c *gin.Context) {
	relativePath := c.Query("path")
	if relativePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	absPath, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if absPath == h.absBase {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete base directory"})
		return
	}

	if err := os.RemoveAll(absPath); err != nil {
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

	absPath, err := h.checkPath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir := filepath.Dir(absPath)
	newPath := filepath.Join(dir, req.Name)

	if !strings.HasPrefix(newPath, h.absBase) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
		return
	}

	if err := os.Rename(absPath, newPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "renamed successfully",
		"path":    h.toRelPath(newPath),
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

	absSrc, err := h.checkPath(req.Src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	absDst, err := h.checkPath(req.Dst)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := os.Rename(absSrc, absDst); err != nil {
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

	absSrc, err := h.checkPath(req.Src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	absDst, err := h.checkPath(req.Dst)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	srcInfo, err := os.Stat(absSrc)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source not found"})
		return
	}

	if srcInfo.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot copy directory"})
		return
	}

	srcFile, err := os.Open(absSrc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dstFile, err := os.Create(absDst)
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

	absPath, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	info, err := os.Stat(absPath)
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

	content, err := os.ReadFile(absPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":    h.toRelPath(absPath),
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

	absPath, err := h.checkPath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := os.WriteFile(absPath, []byte(req.Content), 0644); err != nil {
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

	absPath, err := h.checkPath(relativePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":     h.toRelPath(absPath),
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

