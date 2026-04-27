package handler

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type ServerHandler struct{}

func NewServerHandler() *ServerHandler {
	return &ServerHandler{}
}

type CreateServerRequest struct {
	Name         string `json:"name" binding:"required"`
	IP           string `json:"ip" binding:"required"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSHKey       string `json:"ssh_key"`
	Group        string `json:"group"`
	OsType       string `json:"os_type"`
	ServerType   string `json:"server_type"`
	Description  string `json:"description"`
	JumpEnabled  bool   `json:"jump_enabled"`
	JumpServerID uint   `json:"jump_server_id"`
	JumpIP       string `json:"jump_ip"`
	JumpPort     int    `json:"jump_port"`
	JumpUser     string `json:"jump_user"`
	JumpPassword string `json:"jump_password"`
	JumpKey      string `json:"jump_key"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyType    string `json:"proxy_type"`
	ProxyHost    string `json:"proxy_host"`
	ProxyPort    int    `json:"proxy_port"`
}

type UpdateServerRequest struct {
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSHKey       string `json:"ssh_key"`
	Group        string `json:"group"`
	OsType       string `json:"os_type"`
	ServerType   string `json:"server_type"`
	Description  string `json:"description"`
	JumpEnabled  bool   `json:"jump_enabled"`
	JumpServerID uint   `json:"jump_server_id"`
	JumpIP       string `json:"jump_ip"`
	JumpPort     int    `json:"jump_port"`
	JumpUser     string `json:"jump_user"`
	JumpPassword string `json:"jump_password"`
	JumpKey      string `json:"jump_key"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyType    string `json:"proxy_type"`
	ProxyHost    string `json:"proxy_host"`
	ProxyPort    int    `json:"proxy_port"`
}

type ServerDTO struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	Port         int       `json:"port"`
	Username     string    `json:"username"`
	Group        string    `json:"group"`
	OsType       string    `json:"os_type"`
	ServerType   string    `json:"server_type"`
	Description  string    `json:"description"`
	Status       string    `json:"status"`
	JumpEnabled  bool      `json:"jump_enabled"`
	JumpServerID uint      `json:"jump_server_id"`
	JumpIP       string    `json:"jump_ip"`
	JumpPort     int       `json:"jump_port"`
	JumpUser     string    `json:"jump_user"`
	ProxyEnabled bool      `json:"proxy_enabled"`
	ProxyType    string    `json:"proxy_type"`
	ProxyHost    string    `json:"proxy_host"`
	ProxyPort    int       `json:"proxy_port"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ServerFullDTO struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSHKey       string `json:"ssh_key"`
	Group        string `json:"group"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	JumpEnabled  bool   `json:"jump_enabled"`
	JumpServerID uint   `json:"jump_server_id"`
	JumpIP       string `json:"jump_ip"`
	JumpPort     int    `json:"jump_port"`
	JumpUser     string `json:"jump_user"`
	JumpPassword string `json:"jump_password"`
	JumpKey      string `json:"jump_key"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyType    string `json:"proxy_type"`
	ProxyHost    string `json:"proxy_host"`
	ProxyPort    int    `json:"proxy_port"`
}

func (h *ServerHandler) ListServers(c *gin.Context) {
	sqlDB, err := database.DB.DB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	query := "SELECT id, name, ip, port, username, `group`, os_type, server_type, description, status, jump_enabled, jump_server_id, jump_ip, jump_port, jump_user, proxy_enabled, proxy_type, proxy_host, proxy_port, created_at, updated_at FROM servers WHERE deleted_at IS NULL"
	args := []interface{}{}

	group := c.Query("group")
	if group != "" {
		query += " AND `group` = ?"
		args = append(args, group)
	}

	status := c.Query("status")
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	rows, err := sqlDB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	result := []interface{}{}
	for rows.Next() {
		var id, port, jumpPort, proxyPort int
		var name, ip, username, description, status, createdAt, updatedAt, osType, serverType string
		var group sql.NullString
		var jumpEnabled, proxyEnabled bool
		var jumpServerID uint
		var jumpIP, jumpUser, proxyType, proxyHost string
		err := rows.Scan(&id, &name, &ip, &port, &username, &group, &osType, &serverType, &description, &status, &jumpEnabled, &jumpServerID, &jumpIP, &jumpPort, &jumpUser, &proxyEnabled, &proxyType, &proxyHost, &proxyPort, &createdAt, &updatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		groupStr := ""
		if group.Valid {
			groupStr = group.String
		}
		result = append(result, map[string]interface{}{
			"id":             id,
			"name":           name,
			"ip":             ip,
			"port":           port,
			"username":       username,
			"group":          groupStr,
			"os_type":        osType,
			"server_type":    serverType,
			"description":    description,
			"status":         status,
			"jump_enabled":   jumpEnabled,
			"jump_server_id": jumpServerID,
			"jump_ip":        jumpIP,
			"jump_port":      jumpPort,
			"jump_user":      jumpUser,
			"proxy_enabled":  proxyEnabled,
			"proxy_type":     proxyType,
			"proxy_host":     proxyHost,
			"proxy_port":     proxyPort,
			"created_at":     createdAt,
			"updated_at":     updatedAt,
		})
	}

	w := c.Writer
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"servers":`))
	data, _ := json.Marshal(result)
	w.Write(data)
	w.Write([]byte(`}`))
}

func (h *ServerHandler) GetServer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	sqlDB, err := database.DB.DB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	query := "SELECT id, name, ip, port, username, `group`, os_type, server_type, description, status, jump_enabled, jump_ip, jump_port, jump_user, proxy_enabled, proxy_type, proxy_host, proxy_port, created_at, updated_at FROM servers WHERE id = ? AND deleted_at IS NULL"
	row := sqlDB.QueryRow(query, id)

	var server ServerDTO
	var group sql.NullString
	err = row.Scan(&server.ID, &server.Name, &server.IP, &server.Port, &server.Username, &group, &server.OsType, &server.ServerType, &server.Description, &server.Status, &server.JumpEnabled, &server.JumpIP, &server.JumpPort, &server.JumpUser, &server.ProxyEnabled, &server.ProxyType, &server.ProxyHost, &server.ProxyPort, &server.CreatedAt, &server.UpdatedAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	if group.Valid {
		server.Group = group.String
	}
	if server.OsType == "" {
		server.OsType = "linux"
	}
	c.JSON(http.StatusOK, server)
}

func (h *ServerHandler) GetServerFull(c *gin.Context) {
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

	c.JSON(http.StatusOK, ServerFullDTO{
		ID:           server.ID,
		Name:         server.Name,
		IP:           server.IP,
		Port:         server.Port,
		Username:     server.Username,
		Password:     server.Password,
		SSHKey:       server.SSHKey,
		Group:        server.Group,
		Description:  server.Description,
		Status:       server.Status,
		JumpEnabled:  server.JumpEnabled,
		JumpServerID: server.JumpServerID,
		JumpIP:       server.JumpIP,
		JumpPort:     server.JumpPort,
		JumpUser:     server.JumpUser,
		JumpPassword: server.JumpPassword,
		JumpKey:      server.JumpKey,
		ProxyEnabled: server.ProxyEnabled,
		ProxyType:    server.ProxyType,
		ProxyHost:    server.ProxyHost,
		ProxyPort:    server.ProxyPort,
	})
}

func (h *ServerHandler) CreateServer(c *gin.Context) {
	var req CreateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[CreateServer] 收到请求: jump_enabled=%v, jump_server_id=%d, jump_ip=%s", req.JumpEnabled, req.JumpServerID, req.JumpIP)

	if req.Port == 0 {
		req.Port = 22
	}

	server := model.Server{
		Name:         req.Name,
		IP:           req.IP,
		Port:         req.Port,
		Username:     req.Username,
		Password:     req.Password,
		SSHKey:       req.SSHKey,
		Group:        req.Group,
		OsType:       req.OsType,
		ServerType:   req.ServerType,
		Description:  req.Description,
		JumpEnabled:  req.JumpEnabled,
		JumpServerID: req.JumpServerID,
		JumpIP:       req.JumpIP,
		JumpPort:     req.JumpPort,
		JumpUser:     req.JumpUser,
		JumpPassword: req.JumpPassword,
		JumpKey:      req.JumpKey,
		ProxyEnabled: req.ProxyEnabled,
		ProxyType:    req.ProxyType,
		ProxyHost:    req.ProxyHost,
		ProxyPort:    req.ProxyPort,
	}

	if err := database.DB.Create(&server).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, server)
}

func (h *ServerHandler) UpdateServer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	var req UpdateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[UpdateServer] 收到请求: jump_enabled=%v, jump_server_id=%d, jump_ip=%s", req.JumpEnabled, req.JumpServerID, req.JumpIP)

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IP != "" {
		updates["ip"] = req.IP
	}
	if req.Port > 0 {
		updates["port"] = req.Port
	}
	if req.Username != "" {
		updates["username"] = req.Username
	}
	if req.Password != "" {
		updates["password"] = req.Password
	}
	if req.SSHKey != "" {
		updates["ssh_key"] = req.SSHKey
	}
	if req.Group != "" {
		updates["group"] = req.Group
	}
	if req.OsType != "" {
		updates["os_type"] = req.OsType
	}
	if req.ServerType != "" {
		updates["server_type"] = req.ServerType
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	updates["jump_enabled"] = req.JumpEnabled
	updates["jump_server_id"] = req.JumpServerID
	if req.JumpIP != "" {
		updates["jump_ip"] = req.JumpIP
	}
	if req.JumpPort > 0 {
		updates["jump_port"] = req.JumpPort
	}
	if req.JumpUser != "" {
		updates["jump_user"] = req.JumpUser
	}
	if req.JumpPassword != "" {
		updates["jump_password"] = req.JumpPassword
	}
	if req.JumpKey != "" {
		updates["jump_key"] = req.JumpKey
	}
	updates["proxy_enabled"] = req.ProxyEnabled
	if req.ProxyType != "" {
		updates["proxy_type"] = req.ProxyType
	}
	if req.ProxyHost != "" {
		updates["proxy_host"] = req.ProxyHost
	}
	if req.ProxyPort > 0 {
		updates["proxy_port"] = req.ProxyPort
	}

	if err := database.DB.Model(&server).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, server)
}

func (h *ServerHandler) DeleteServer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	if err := database.DB.Delete(&model.Server{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	service.SSHSvc.RemoveClient(uint(id))
	c.JSON(http.StatusOK, gin.H{"message": "server deleted successfully"})
}

func (h *ServerHandler) TestConnection(c *gin.Context) {
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

	start := time.Now()
	err = service.SSHSvc.TestConnection(&server)
	duration := time.Since(start)

	if err != nil {
		if err := database.DB.Model(&server).Update("status", "offline").Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  false,
			"error":    err.Error(),
			"duration": duration.String(),
		})
		return
	}

	if err := database.DB.Model(&server).Update("status", "online").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"duration": duration.String(),
	})
}

func (h *ServerHandler) CheckAllServers(c *gin.Context) {
	var servers []model.Server
	database.DB.Find(&servers)

	results := make(map[uint]bool)
	for _, server := range servers {
		err := service.SSHSvc.TestConnection(&server)
		if err == nil {
			results[server.ID] = true
			database.DB.Model(&server).Update("status", "online")
		} else {
			results[server.ID] = false
			database.DB.Model(&server).Update("status", "offline")
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (h *ServerHandler) GetServerGroups(c *gin.Context) {
	var groups []string
	database.DB.Model(&model.Server{}).Distinct("group").Find(&groups)
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

type ExecRequest struct {
	Command string `json:"command" binding:"required"`
	Timeout int    `json:"timeout"`
}

func (h *ServerHandler) ExecCommand(c *gin.Context) {
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

	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	timeout := 30 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	output, err := service.SSHSvc.ExecuteCommand(&server, req.Command, timeout)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"output": "", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"output": output})
}
