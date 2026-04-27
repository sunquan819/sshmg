package handler

import (
	"log"
	"net/http"
	"strconv"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type TunnelHandler struct{}

func NewTunnelHandler() *TunnelHandler {
	return &TunnelHandler{}
}

type TunnelRequest struct {
	ServerID    uint   `json:"server_id" binding:"required"`
	LocalPort   int    `json:"local_port"`
	BindAddress string `json:"bind_address"`
}

func (h *TunnelHandler) StartTunnel(c *gin.Context) {
	var req TunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, req.ServerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	bindAddr := req.BindAddress
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	log.Printf("Starting tunnel: server=%s (%s), bind=%s, user=%s", server.Name, server.IP, bindAddr, server.Username)

	port, err := service.StartDynamicTunnel(&server, req.LocalPort, bindAddr)
	if err != nil {
		log.Printf("Tunnel error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Tunnel started successfully: port=%d", port)

	c.JSON(http.StatusOK, gin.H{
		"local_port": port,
		"status":     "running",
		"message":    "SOCKS 代理已启动，请配置浏览器或工具使用 localhost:" + strconv.Itoa(port) + " 作为 SOCKS5 代理",
	})
}

func (h *TunnelHandler) StopTunnel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	if err := service.StopTunnel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tunnel stopped"})
}

func (h *TunnelHandler) ListTunnels(c *gin.Context) {
	tunnels := service.ListTunnels()
	c.JSON(http.StatusOK, gin.H{"tunnels": tunnels})
}

func (h *TunnelHandler) GetTunnel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	tunnel := service.GetTunnel(uint(id))
	if tunnel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tunnel": tunnel})
}
