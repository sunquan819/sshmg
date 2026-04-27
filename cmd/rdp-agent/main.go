package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
)

var defaultPort = 8765

func main() {
	port := defaultPort
	if p := os.Getenv("RDP_AGENT_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	logFile, err := os.OpenFile("rdp-agent.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("RDP Agent 启动中，监听端口: %d", port)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/connect", handleConnect)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	log.Printf("RDP Agent 已启动，访问 http://localhost:%d", port)

	select {}
}

type ConnectRequest struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	JumpEnabled  bool   `json:"jump_enabled"`
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

func handleConnect(c *gin.Context) {
	var req ConnectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Port == 0 {
		req.Port = 3389
	}
	if req.JumpPort == 0 {
		req.JumpPort = 22
	}
	if req.ProxyPort == 0 {
		req.ProxyPort = 1080
	}

	log.Printf("收到 RDP 连接请求: %s:%d, 用户: %s, jump=%v, proxy=%v",
		req.Host, req.Port, req.Username, req.JumpEnabled, req.ProxyEnabled)

	var localPort int
	var targetHost string
	var err error

	if req.JumpEnabled && req.JumpIP != "" {
		localPort, targetHost, err = connectViaJump(req)
	} else if req.ProxyEnabled && req.ProxyHost != "" {
		localPort, targetHost, err = connectViaProxy(req)
	} else {
		localPort, targetHost, err = connectDirect(req)
	}

	if err != nil {
		log.Printf("RDP 连接失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "RDP 连接已启动",
		"local_ip":   "127.0.0.1",
		"local_port": localPort,
		"target":     targetHost,
	})
}

func connectDirect(req ConnectRequest) (int, string, error) {
	target := fmt.Sprintf("%s:%d", req.Host, req.Port)
	rdpFile := os.Getenv("TEMP") + "\\rdp_connect.rdp"

	rdpContent := fmt.Sprintf("full address:s:%s\r\nusername:s:%s\r\nprompt for credentials:i:0\r\nauthentication level:i:0\r\n",
		target, req.Username)

	exec.Command("cmdkey", "/generic:TERMSRV/"+req.Host, "/user:"+req.Username, "/pass:"+req.Password).Run()

	if err := os.WriteFile(rdpFile, []byte(rdpContent), 0644); err != nil {
		return 0, "", fmt.Errorf("创建 RDP 文件失败: %v", err)
	}

	log.Printf("RDP 直连: %s", target)

	mstscCmd := exec.Command("mstsc", rdpFile)
	if err := mstscCmd.Start(); err != nil {
		return 0, "", fmt.Errorf("启动 mstsc 失败: %v", err)
	}

	log.Printf("RDP 连接已启动: %s", target)
	return req.Port, target, nil
}

func getLocalIPWithGateway() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				ip := ipNet.IP.String()
				if !strings.HasPrefix(ip, "169.254") && !strings.HasPrefix(ip, "127.") && !strings.HasPrefix(ip, "fe80") {
					log.Printf("选择网卡IP: %s (%s)", ip, iface.Name)
					return ip
				}
			}
		}
	}
	log.Printf("未找到合适的网卡，使用 127.0.0.1")
	return "127.0.0.1"
}

func connectViaJump(req ConnectRequest) (int, string, error) {
	log.Printf("使用跳板机: %s:%d -> %s:%d", req.JumpIP, req.JumpPort, req.Host, req.Port)

	target := fmt.Sprintf("%s:%d", req.Host, req.Port)
	localIP := getLocalIPWithGateway()
	log.Printf("选择本地IP: %s", localIP)

	forwardedPort, err := startSSHTunnel(req.JumpIP, req.JumpPort, req.JumpUser, req.JumpPassword, req.JumpKey, req.Host, 3389)
	if err != nil {
		return 0, "", fmt.Errorf("SSH 隧道失败: %v", err)
	}

	log.Printf("SSH 隧道建立: localhost:%d -> %s:3389", forwardedPort, req.Host)

	connectTarget := fmt.Sprintf("127.0.0.1:%d", forwardedPort)
	connectHost := "127.0.0.1"
	exec.Command("cmdkey", "/generic:TERMSRV/"+connectHost, "/user:"+req.Username, "/pass:"+req.Password).Run()

	rdpFile := os.Getenv("TEMP") + "\\rdp_connect.rdp"
	rdpContent := fmt.Sprintf(
		"full address:s:%s\r\n"+
			"username:s:%s\r\n"+
			"prompt for credentials:i:0\r\n"+
			"authentication level:i:2\r\n",
		connectTarget, req.Username)

	if err := os.WriteFile(rdpFile, []byte(rdpContent), 0644); err != nil {
		return 0, "", fmt.Errorf("创建 RDP 文件失败: %v", err)
	}

	log.Printf("RDP 跳板机连接: %s -> %s", connectTarget, target)

	mstscCmd := exec.Command("mstsc", rdpFile)
	if err := mstscCmd.Start(); err != nil {
		return 0, "", fmt.Errorf("启动 mstsc 失败: %v", err)
	}

	log.Printf("RDP 连接已启动: %s -> %s", connectTarget, target)
	return forwardedPort, connectTarget, nil
}

func connectViaProxy(req ConnectRequest) (int, string, error) {
	log.Printf("使用代理: %s:%d (type: %s)", req.ProxyHost, req.ProxyPort, req.ProxyType)

	target := fmt.Sprintf("%s:%d", req.Host, req.Port)
	localPort := 13388
	localIP := getLocalIPWithGateway()
	log.Printf("选择本地IP: %s", localIP)

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", localPort))
	if err != nil {
		log.Printf("端口 %d 被占用，尝试新端口", localPort)
		for i := 0; i < 100; i++ {
			localPort = 13388 + i
			listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", localIP, localPort))
			if err == nil {
				break
			}
		}
		if err != nil {
			return 0, "", fmt.Errorf("无可用端口: %v", err)
		}
	}

	log.Printf("本地代理中转端口: %d -> %s (经过 %s:%d)", localPort, target, req.ProxyHost, req.ProxyPort)

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				break
			}
			go handleProxyConn(localConn, req.ProxyHost, req.ProxyPort, req.ProxyType, target)
		}
	}()

	connectTarget := fmt.Sprintf("127.0.0.1:%d", localPort)
	exec.Command("cmdkey", "/generic:TERMSRV/127.0.0.1", "/user:"+req.Username, "/pass:"+req.Password).Run()

	rdpFile := os.Getenv("TEMP") + "\\rdp_connect.rdp"

	rdpContent := fmt.Sprintf(
		"full address:s:%s\r\n"+
			"username:s:%s\r\n"+
			"prompt for credentials:i:0\r\n"+
			"authentication level:i:2\r\n",
		connectTarget, req.Username)

	if err := os.WriteFile(rdpFile, []byte(rdpContent), 0644); err != nil {
		return 0, "", fmt.Errorf("创建 RDP 文件失败: %v", err)
	}

	log.Printf("RDP 代理连接: %s -> %s", connectTarget, target)

	mstscCmd := exec.Command("mstsc", rdpFile)
	if err := mstscCmd.Start(); err != nil {
		return 0, "", fmt.Errorf("启动 mstsc 失败: %v", err)
	}

	log.Printf("RDP 连接已启动: %s -> %s", connectTarget, target)
	return localPort, connectTarget, nil
}

func startSSHTunnel(host string, port int, user, password, key, targetHost string, targetPort int) (int, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{},
	}

	if password != "" {
		config.Auth = append(config.Auth, ssh.Password(password))
	}
	if key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err == nil {
			config.Auth = append(config.Auth, ssh.PublicKeys(signer))
		}
	}

	config.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return 0, fmt.Errorf("连接 SSH 失败: %v", err)
	}

	localAddr := "127.0.0.1:0"
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		client.Close()
		return 0, fmt.Errorf("监听本地端口失败: %v", err)
	}

	localPort := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				break
			}

			go func() {
				defer conn.Close()
				clientConn, err := client.Dial("tcp", fmt.Sprintf("%s:%d", targetHost, targetPort))
				if err != nil {
					log.Printf("连接目标失败: %v", err)
					return
				}
				defer clientConn.Close()

				go io.Copy(clientConn, conn)
				io.Copy(conn, clientConn)
			}()
		}
	}()

	log.Printf("SSH 隧道: localhost:%d -> %s:%d", localPort, targetHost, targetPort)
	return localPort, nil
}

func handleProxyConn(localConn net.Conn, proxyHost string, proxyPort int, proxyType string, target string) {
	defer localConn.Close()

	proxyAddr := fmt.Sprintf("%s:%d", proxyHost, proxyPort)
	log.Printf("连接代理服务器: %s", proxyAddr)

	var remoteConn net.Conn
	var err error

	if proxyType == "socks5" {
		remoteConn, err = net.Dial("tcp", proxyAddr)
		if err != nil {
			log.Printf("连接 SOCKS5 代理 %s 失败: %v", proxyAddr, err)
			return
		}

		remoteConn.Write([]byte{0x05, 0x01, 0x00})
		resp := make([]byte, 2)
		n, _ := remoteConn.Read(resp)
		log.Printf("SOCKS5 认证响应: %v (n=%d)", resp[:n], n)
		if n < 2 || resp[0] != 0x05 || resp[1] != 0x00 {
			log.Printf("SOCKS5 认证失败")
			return
		}

		host, portStr, _ := net.SplitHostPort(target)
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		ip := net.ParseIP(host)
		if ip == nil {
			ips, err := net.LookupIP(host)
			if err != nil || len(ips) == 0 {
				log.Printf("无法解析主机: %s", host)
				return
			}
			ip = ips[0]
			log.Printf("解析 %s -> %s", host, ip.String())
		}

		req := []byte{0x05, 0x01, 0x00, 0x01}
		req = append(req, ip.To4()...)
		req = append(req, []byte{byte(port >> 8), byte(port & 0xff)}...)
		remoteConn.Write(req)

		resp = make([]byte, 10)
		n, _ = remoteConn.Read(resp)
		log.Printf("SOCKS5 连接响应: %v (n=%d)", resp[:n], n)
		if n < 2 || resp[1] != 0x00 {
			log.Printf("SOCKS5 连接失败")
			return
		}
		log.Printf("SOCKS5 代理连接成功 -> %s", target)
	} else if proxyType == "http" {
		remoteConn, err = net.Dial("tcp", proxyAddr)
		if err != nil {
			log.Printf("连接 HTTP 代理失败: %v", err)
			return
		}

		req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		remoteConn.Write([]byte(req))

		buf := make([]byte, 1024)
		remoteConn.Read(buf)
		log.Printf("HTTP 代理连接成功 -> %s", target)
	} else {
		log.Printf("不支持的代理类型: %s", proxyType)
		return
	}

	defer remoteConn.Close()

	log.Printf("开始转发流量: %s <-> %s", localConn.RemoteAddr(), remoteConn.RemoteAddr())

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := localConn.Read(buf)
			if err != nil {
				log.Printf("本地连接读取结束: %v", err)
				break
			}
			log.Printf("本地 -> 代理: %d bytes", n)
			_, err = remoteConn.Write(buf[:n])
			if err != nil {
				log.Printf("写入代理失败: %v", err)
				break
			}
		}
	}()

	buf := make([]byte, 32*1024)
	for {
		n, err := remoteConn.Read(buf)
		if err != nil {
			log.Printf("代理连接读取结束: %v", err)
			break
		}
		log.Printf("代理 -> 本地: %d bytes", n)
		_, err = localConn.Write(buf[:n])
		if err != nil {
			log.Printf("写入本地失败: %v", err)
			break
		}
	}

	log.Printf("代理连接关闭")
}
