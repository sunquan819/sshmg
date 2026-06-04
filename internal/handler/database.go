package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	sshPkg "deploy-manager/pkg/ssh"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

type DatabaseHandler struct{}

func getDialerWithProxy(db *model.Database) *net.Dialer {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	if db.ProxyEnabled && db.ProxyHost != "" {
		log.Printf("[DB] 使用代理: %s:%d -> %s:%d (类型: %s)", db.ProxyHost, db.ProxyPort, db.Host, db.Port, db.ProxyType)
	} else if db.JumpEnabled && db.JumpHost != "" {
		log.Printf("[DB] 使用跳板机: %s:%d -> %s:%d", db.JumpHost, db.JumpPort, db.Host, db.Port)
	} else {
		log.Printf("[DB] 直连: %s:%d", db.Host, db.Port)
	}
	return dialer
}

type proxyDialer struct {
	net.Dialer
	proxyHost string
	proxyPort int
	proxyType string
}

func (p *proxyDialer) Dial(network, address string) (net.Conn, error) {
	conn, err := p.Dialer.Dial("tcp", fmt.Sprintf("%s:%d", p.proxyHost, p.proxyPort))
	if err != nil {
		return nil, err
	}

	targetHost, targetPort, _ := net.SplitHostPort(address)
	targetAddr := fmt.Sprintf("%s:%s", targetHost, targetPort)
	portNum := 0
	fmt.Sscanf(targetPort, "%d", &portNum)

	if p.proxyType == "http" {
		req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)
		if _, err := conn.Write([]byte(req)); err != nil {
			conn.Close()
			return nil, err
		}
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		resp := string(buf[:n])
		if !strings.Contains(resp, "200") {
			conn.Close()
			return nil, fmt.Errorf("http proxy connect failed")
		}
		return conn, nil
	}

	buf := []byte{5, 1, 0}
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[0] != 5 || resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth failed")
	}
	hostBytes := []byte{byte(len(targetHost))}
	hostBytes = append(hostBytes, []byte(targetHost)...)
	portBytes := []byte{byte(portNum >> 8), byte(portNum & 0xff)}
	req := []byte{5, 1, 0, 3}
	req = append(req, hostBytes...)
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	resp = make([]byte, 10)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed")
	}
	return conn, nil
}

func NewDatabaseHandler() *DatabaseHandler {
	return &DatabaseHandler{}
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

func (h *DatabaseHandler) ListDatabases(c *gin.Context) {
	var databases []model.Database
	query := database.DB.Model(&model.Database{})

	group := c.Query("group")
	if group != "" {
		if group == "__ungrouped__" {
			query = query.Where("`group` IS NULL OR `group` = ''")
		} else {
			query = query.Where("`group` = ?", group)
		}
	}

	if err := query.Order("created_at DESC").Find(&databases).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"databases": databases})
}

func (h *DatabaseHandler) GetDatabase(c *gin.Context) {
	id := c.Param("id")
	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"database": db})
}

type CreateDatabaseRequest struct {
	Name           string `json:"name" binding:"required"`
	Type           string `json:"type" binding:"required"`
	Host           string `json:"host" binding:"required"`
	Port           int    `json:"port" binding:"required"`
	Database       string `json:"database"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	SSLMode        string `json:"ssl_mode"`
	Description    string `json:"description"`
	Group          string `json:"group"`
	ShowAllSchemas bool   `json:"show_all_schemas"`
	JumpEnabled    bool   `json:"jump_enabled"`
	JumpServerID   uint   `json:"jump_server_id"`
	JumpHost       string `json:"jump_host"`
	JumpPort       int    `json:"jump_port"`
	JumpUser       string `json:"jump_user"`
	JumpPassword   string `json:"jump_password"`
	JumpKey        string `json:"jump_key"`
	ProxyEnabled   bool   `json:"proxy_enabled"`
	ProxyServerID  uint   `json:"proxy_server_id"`
	ProxyType      string `json:"proxy_type"`
	ProxyHost      string `json:"proxy_host"`
	ProxyPort      int    `json:"proxy_port"`
}

type UpdateDatabaseRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Database       string `json:"database"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	SSLMode        string `json:"ssl_mode"`
	Description    string `json:"description"`
	Group          string `json:"group"`
	ShowAllSchemas bool   `json:"show_all_schemas"`
	JumpEnabled    bool   `json:"jump_enabled"`
	JumpServerID   uint   `json:"jump_server_id"`
	JumpHost       string `json:"jump_host"`
	JumpPort       int    `json:"jump_port"`
	JumpUser       string `json:"jump_user"`
	JumpPassword   string `json:"jump_password"`
	JumpKey        string `json:"jump_key"`
	ProxyEnabled   bool   `json:"proxy_enabled"`
	ProxyServerID  uint   `json:"proxy_server_id"`
	ProxyType      string `json:"proxy_type"`
	ProxyHost      string `json:"proxy_host"`
	ProxyPort      int    `json:"proxy_port"`
}

func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	var req CreateDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jumpHost, jumpPort, jumpUser, jumpPassword, jumpKey := req.JumpHost, req.JumpPort, req.JumpUser, req.JumpPassword, req.JumpKey
	proxyHost, proxyPort := req.ProxyHost, req.ProxyPort

	if req.JumpServerID > 0 {
		var server model.Server
		if err := database.DB.First(&server, req.JumpServerID).Error; err == nil {
			jumpHost = server.IP
			jumpPort = server.Port
			jumpUser = server.Username
			jumpPassword = server.Password
			jumpKey = server.SSHKey
		}
	}

	if req.ProxyServerID > 0 {
		var server model.Server
		if err := database.DB.First(&server, req.ProxyServerID).Error; err == nil {
			proxyHost = server.IP
			proxyPort = 7890
		}
	}

	db := model.Database{
		Name:           req.Name,
		Type:           req.Type,
		Host:           req.Host,
		Port:           req.Port,
		Database:       req.Database,
		Username:       req.Username,
		Password:       req.Password,
		SSLMode:        req.SSLMode,
		Description:    req.Description,
		Group:          req.Group,
		ShowAllSchemas: req.ShowAllSchemas,
		Status:         "unknown",
		JumpEnabled:    req.JumpEnabled,
		JumpServerID:   req.JumpServerID,
		JumpHost:       jumpHost,
		JumpPort:       jumpPort,
		JumpUser:       jumpUser,
		JumpPassword:   jumpPassword,
		JumpKey:        jumpKey,
		ProxyEnabled:   req.ProxyEnabled,
		ProxyServerID:  req.ProxyServerID,
		ProxyType:      req.ProxyType,
		ProxyHost:      proxyHost,
		ProxyPort:      proxyPort,
	}

	if err := database.DB.Create(&db).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"database": db})
}

func (h *DatabaseHandler) UpdateDatabase(c *gin.Context) {
	id := c.Param("id")
	log.Printf("[UpdateDatabase] Request ID: %s", id)

	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		log.Printf("[UpdateDatabase] Database not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	var req UpdateDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[UpdateDatabase] BindJSON error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[UpdateDatabase] Request: %+v", req)

	jumpHost, jumpPort, jumpUser, jumpPassword, jumpKey := req.JumpHost, req.JumpPort, req.JumpUser, req.JumpPassword, req.JumpKey
	proxyHost, proxyPort := req.ProxyHost, req.ProxyPort

	if req.JumpServerID > 0 {
		var server model.Server
		if err := database.DB.First(&server, req.JumpServerID).Error; err == nil {
			jumpHost = server.IP
			jumpPort = server.Port
			jumpUser = server.Username
			jumpPassword = server.Password
			jumpKey = server.SSHKey
		}
	}

	if req.ProxyServerID > 0 {
		var server model.Server
		if err := database.DB.First(&server, req.ProxyServerID).Error; err == nil {
			proxyHost = server.IP
			proxyPort = 7890
		}
	} else if req.ProxyEnabled && req.ProxyHost == "" && db.ProxyHost != "" {
		proxyHost = db.ProxyHost
		proxyPort = db.ProxyPort
	} else if req.ProxyHost != "" {
		proxyHost = req.ProxyHost
		proxyPort = req.ProxyPort
	}

	updates := map[string]interface{}{
		"name":             req.Name,
		"type":             req.Type,
		"host":             req.Host,
		"port":             req.Port,
		"database":         req.Database,
		"username":         req.Username,
		"password":         req.Password,
		"ssl_mode":         req.SSLMode,
		"description":      req.Description,
		"group":            req.Group,
		"show_all_schemas": req.ShowAllSchemas,
		"jump_enabled":     req.JumpEnabled,
		"jump_server_id":   req.JumpServerID,
		"jump_host":        jumpHost,
		"jump_port":        jumpPort,
		"jump_user":        jumpUser,
		"jump_password":    jumpPassword,
		"jump_key":         jumpKey,
		"proxy_enabled":    req.ProxyEnabled,
		"proxy_server_id":  req.ProxyServerID,
		"proxy_type":       req.ProxyType,
		"proxy_host":       proxyHost,
		"proxy_port":       proxyPort,
	}

	if err := database.DB.Model(&db).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	database.DB.First(&db, id)
	c.JSON(http.StatusOK, gin.H{"database": db})
}

func (h *DatabaseHandler) DeleteDatabase(c *gin.Context) {
	id := c.Param("id")
	if err := database.DB.Delete(&model.Database{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

func (h *DatabaseHandler) TestConnection(c *gin.Context) {
	id := c.Param("id")
	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	err := testDBConnection(&db)
	if err != nil {
		db.Status = "failed"
		database.DB.Model(&db).Update("status", "failed")
		c.JSON(http.StatusOK, gin.H{"status": "failed", "error": err.Error()})
		return
	}

	db.Status = "connected"
	database.DB.Model(&db).Update("status", "connected")
	c.JSON(http.StatusOK, gin.H{"status": "connected"})
}

type TestConnectionRequest struct {
	Type     string `json:"type" binding:"required"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
}

func (h *DatabaseHandler) TestConnectionDirect(c *gin.Context) {
	var req TestConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	db := &model.Database{
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		Database: req.Database,
		Username: req.Username,
		Password: req.Password,
		SSLMode:  req.SSLMode,
	}

	err := testDBConnection(db)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "connected"})
}

func testDBConnection(db *model.Database) error {
	// Kafka 测试连接
	if db.Type == "kafka" {
		log.Printf("Kafka test: host=%s, port=%d", db.Host, db.Port)
		err := testKafkaConnection(db)
		log.Printf("Kafka test result: %v", err)
		return err
	}

	// Redis 测试连接
	if db.Type == "redis" {
		maskedPassword := "***"
		if db.Password != "" {
			maskedPassword = "***"
		}
		log.Printf("Redis test: host=%s, port=%d, password='%s', db=%s", db.Host, db.Port, maskedPassword, db.Database)
		rdb := getRedisClient(db)
		defer rdb.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := rdb.Ping(ctx).Err()
		log.Printf("Redis test result: %v", err)
		return err
	}

	sqlDB, err := getSQLDBWithProxy(db)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return sqlDB.PingContext(ctx)
}

func getSQLDBWithProxy(db *model.Database) (*sql.DB, error) {
	dsn := getDSN(db)

	if db.JumpEnabled && db.JumpHost != "" {
		log.Printf("[DB] 使用跳板机 SSH 隧道: %s:%d -> %s:%d", db.JumpHost, db.JumpPort, db.Host, db.Port)
		sshClient := sshPkg.NewClient(db.JumpHost, db.JumpPort, db.JumpUser, db.JumpPassword, db.JumpKey)
		if err := sshClient.Connect(); err != nil {
			return nil, fmt.Errorf("跳板机连接失败: %w", err)
		}
		localPort, err := sshClient.LocalPortForward(db.Host, db.Port)
		if err != nil {
			sshClient.Close()
			return nil, fmt.Errorf("端口转发失败: %w", err)
		}
		log.Printf("[DB] SSH 隧道建立: localhost:%d -> %s:%d", localPort, db.Host, db.Port)
		dsn = strings.Replace(dsn, fmt.Sprintf("%s:%d", db.Host, db.Port), fmt.Sprintf("127.0.0.1:%d", localPort), 1)
		defer sshClient.Close()
	}

	var sqlDB *sql.DB
	var err error

	switch db.Type {
	case "mysql":
		sqlDB, err = sql.Open("mysql", dsn)
	case "postgresql", "postgres":
		sqlDB, err = sql.Open("postgres", dsn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.Type)
	}

	if err != nil {
		return nil, err
	}

	if db.ProxyEnabled && db.ProxyHost != "" {
		log.Printf("[DB] 使用代理: %s:%d -> %s:%d (类型: %s)", db.ProxyHost, db.ProxyPort, db.Host, db.Port, db.ProxyType)
		proxyConn, err := dialViaProxy(fmt.Sprintf("%s:%d", db.Host, db.Port), db.ProxyHost, db.ProxyPort, db.ProxyType)
		if err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("代理连接失败: %w", err)
		}
		log.Printf("[DB] 代理连接成功")
		_ = proxyConn
	}

	return sqlDB, nil
}

func getDSN(db *model.Database) string {
	dbName := db.Database
	if dbName == "" {
		if db.Type == "mysql" {
			dbName = "mysql"
		} else if db.Type == "postgresql" || db.Type == "postgres" {
			dbName = "postgres"
		}
	}
	switch db.Type {
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			db.Username, url.QueryEscape(db.Password), db.Host, db.Port, dbName)
	case "postgresql", "postgres":
		sslmode := "disable"
		if db.SSLMode != "" {
			sslmode = db.SSLMode
		}
		// 密码中的单引号用两个单引号转义
		escapedPassword := strings.ReplaceAll(db.Password, "'", "''")
		return fmt.Sprintf("host=%s port=%d user=%s password='%s' dbname=%s sslmode=%s",
			db.Host, db.Port, db.Username, escapedPassword, dbName, sslmode)
	}
	return ""
}

func getRedisClient(db *model.Database) *redis.Client {
	redisDB := 0
	if db.Database != "" {
		if n, err := strconv.Atoi(db.Database); err == nil {
			redisDB = n
		}
	}

	addr := fmt.Sprintf("%s:%d", db.Host, db.Port)

	if db.JumpEnabled && db.JumpHost != "" {
		log.Printf("[Redis] 使用跳板机 SSH 隧道: %s:%d -> %s", db.JumpHost, db.JumpPort, addr)
		sshClient := sshPkg.NewClient(db.JumpHost, db.JumpPort, db.JumpUser, db.JumpPassword, db.JumpKey)
		if err := sshClient.Connect(); err != nil {
			log.Printf("[Redis] 跳板机连接失败: %v", err)
		} else {
			localPort, err := sshClient.LocalPortForward(db.Host, db.Port)
			if err != nil {
				log.Printf("[Redis] 端口转发失败: %v", err)
			} else {
				log.Printf("[Redis] SSH 隧道建立: localhost:%d -> %s", localPort, addr)
				addr = fmt.Sprintf("127.0.0.1:%d", localPort)
				defer sshClient.Close()
			}
		}
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: db.Password,
		DB:       redisDB,
	}

	if db.ProxyEnabled && db.ProxyHost != "" {
		log.Printf("[Redis] 使用代理: %s:%d -> %s (类型: %s)", db.ProxyHost, db.ProxyPort, addr, db.ProxyType)
		proxyDialer := &proxyDialer{
			Dialer:    net.Dialer{Timeout: 10 * time.Second},
			proxyHost: db.ProxyHost,
			proxyPort: db.ProxyPort,
			proxyType: db.ProxyType,
		}
		opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return proxyDialer.Dial("tcp", addr)
		}
	}

	return redis.NewClient(opts)
}

func testRedisConnection(db *model.Database) error {
	rdb := getRedisClient(db)
	defer rdb.Close()
	return rdb.Ping(context.Background()).Err()
}

func getKafkaReader(db *model.Database, topic string) *kafka.Reader {
	readerTopic := topic
	if readerTopic == "" {
		readerTopic = db.Database
	}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{fmt.Sprintf("%s:%d", db.Host, db.Port)},
		Topic:    readerTopic,
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	return reader
}

func testKafkaConnection(db *model.Database) error {
	conn, err := dialKafkaWithProxy(db)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Brokers()
	return err
}

func dialKafkaWithProxy(db *model.Database) (*kafka.Conn, error) {
	addr := fmt.Sprintf("%s:%d", db.Host, db.Port)

	if db.JumpEnabled && db.JumpHost != "" {
		log.Printf("[Kafka] 使用跳板机 SSH 隧道: %s:%d -> %s:%d", db.JumpHost, db.JumpPort, db.Host, db.Port)
		sshClient := sshPkg.NewClient(db.JumpHost, db.JumpPort, db.JumpUser, db.JumpPassword, db.JumpKey)
		if err := sshClient.Connect(); err != nil {
			return nil, fmt.Errorf("跳板机连接失败: %w", err)
		}
		localPort, err := sshClient.LocalPortForward(db.Host, db.Port)
		if err != nil {
			sshClient.Close()
			return nil, fmt.Errorf("端口转发失败: %w", err)
		}
		log.Printf("[Kafka] SSH 隧道建立: localhost:%d -> %s:%d", localPort, db.Host, db.Port)
		conn, err := kafka.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err != nil {
			sshClient.Close()
			return nil, err
		}
		return conn, nil
	}

	if db.ProxyEnabled && db.ProxyHost != "" {
		log.Printf("[Kafka] 使用代理: %s:%d -> %s (类型: %s)", db.ProxyHost, db.ProxyPort, addr, db.ProxyType)
		conn, err := dialViaProxy(addr, db.ProxyHost, db.ProxyPort, db.ProxyType)
		if err != nil {
			return nil, fmt.Errorf("代理连接失败: %w", err)
		}
		kafkaConn := kafka.NewConn(conn, "", 0)
		return kafkaConn, nil
	}

	log.Printf("[Kafka] 直连: %s", addr)
	conn, err := kafka.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func dialViaProxy(targetAddr, proxyHost string, proxyPort int, proxyType string) (net.Conn, error) {
	proxyAddr := fmt.Sprintf("%s:%d", proxyHost, proxyPort)

	if proxyType == "http" {
		conn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			return nil, err
		}
		req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)
		if _, err := conn.Write([]byte(req)); err != nil {
			conn.Close()
			return nil, err
		}
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		resp := string(buf[:n])
		if !strings.Contains(resp, "200") {
			conn.Close()
			return nil, fmt.Errorf("http proxy connect failed: %s", resp)
		}
		return conn, nil
	}

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}

	buf := []byte{5, 1, 0}
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[0] != 5 || resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth failed")
	}

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	portNum := 0
	fmt.Sscanf(targetPort, "%d", &portNum)
	hostBytes := []byte{byte(len(targetHost))}
	hostBytes = append(hostBytes, []byte(targetHost)...)
	portBytes := []byte{byte(portNum >> 8), byte(portNum & 0xff)}
	req := []byte{5, 1, 0, 3}
	req = append(req, hostBytes...)
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	resp = make([]byte, 10)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed")
	}
	return conn, nil
}

func executeKafkaQuery(db *model.Database, query string) ([]map[string]interface{}, error) {
	args := strings.Fields(query)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty query")
	}

	cmd := strings.ToUpper(args[0])
	topic := db.Database
	if topic == "" && len(args) > 1 {
		topic = args[1]
	}

	switch cmd {
	case "CONSUME", "READ":
		// 消费消息: CONSUME [topic] [count] 或 CONSUME [count]
		count := 10
		topicArg := topic
		if len(args) > 1 {
			// 检查第一个参数是数字还是 topic
			if n, err := strconv.Atoi(args[1]); err == nil {
				count = n
			} else {
				topicArg = args[1]
				if len(args) > 2 {
					if n, err := strconv.Atoi(args[2]); err == nil {
						count = n
					}
				}
			}
		}
		if topicArg == "" {
			return nil, fmt.Errorf("请指定 topic: CONSUME <topic> [count]")
		}
		conn, err := dialKafkaWithProxy(db)
		if err != nil {
			return nil, err
		}
		defer conn.Close()

		reader := getKafkaReader(db, topicArg)
		defer reader.Close()

		var results []map[string]interface{}
		for i := 0; i < count; i++ {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				break
			}
			results = append(results, map[string]interface{}{
				"partition": msg.Partition,
				"offset":    msg.Offset,
				"key":       string(msg.Key),
				"value":     string(msg.Value),
				"timestamp": msg.Time.Format(time.RFC3339),
			})
		}
		return results, nil
	case "LIST_TOPICS":
		topics, err := getKafkaSchemas(db)
		if err != nil {
			return nil, err
		}
		var results []map[string]interface{}
		for _, t := range topics {
			results = append(results, map[string]interface{}{"topic": t})
		}
		return results, nil
	case "TOPIC_INFO":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: TOPIC_INFO <topic>")
		}
		targetTopic := args[1]
		conn, err := dialKafkaWithProxy(db)
		if err != nil {
			return nil, err
		}
		defer conn.Close()

		partitions, err := conn.ReadPartitions(targetTopic)
		if err != nil {
			return nil, err
		}

		var results []map[string]interface{}
		seen := make(map[int]bool)
		for _, p := range partitions {
			if !seen[p.ID] {
				seen[p.ID] = true
				results = append(results, map[string]interface{}{
					"partition": p.ID,
					"leader":    p.Leader,
					"replicas":  len(p.Replicas),
					"isr":       len(p.Isr),
				})
			}
		}
		return results, nil
	case "BROKERS":
		conn, err := dialKafkaWithProxy(db)
		if err != nil {
			return nil, err
		}
		defer conn.Close()

		brokers, err := conn.Brokers()
		if err != nil {
			return nil, err
		}

		var results []map[string]interface{}
		for _, b := range brokers {
			results = append(results, map[string]interface{}{
				"host": b.Host,
				"port": b.Port,
				"id":   b.ID,
			})
		}
		return results, nil
	case "CREATE_TOPIC":
		if len(args) < 2 {
			return nil, fmt.Errorf("用法: CREATE_TOPIC <topic> [partitions] [replication]")
		}
		newTopic := args[1]
		partitions := 1
		replication := 1
		if len(args) > 2 {
			if n, err := strconv.Atoi(args[2]); err == nil {
				partitions = n
			}
		}
		if len(args) > 3 {
			if n, err := strconv.Atoi(args[3]); err == nil {
				replication = n
			}
		}
		conn, err := dialKafkaWithProxy(db)
		if err != nil {
			return nil, err
		}
		defer conn.Close()

		topicConfigs := []kafka.TopicConfig{
			{
				Topic:             newTopic,
				NumPartitions:     partitions,
				ReplicationFactor: replication,
			},
		}
		err = conn.CreateTopics(topicConfigs...)
		if err != nil {
			return nil, err
		}
		return []map[string]interface{}{{"result": "Topic created: " + newTopic}}, nil
	case "DELETE_TOPIC":
		return []map[string]interface{}{{"info": "删除 topic 需要使用 kafka-admin 工具或配置"}}, nil
	default:
		return []map[string]interface{}{
			{"info": "Supported commands: CONSUME [topic] [count], LIST_TOPICS, TOPIC_INFO <topic>, BROKERS, CREATE_TOPIC <topic> [partitions] [replication], DELETE_TOPIC <topic>"},
		}, nil
	}
}

var ctx = context.Background()

type QueryRequest struct {
	Query  string `json:"query" binding:"required"`
	Limit  int    `json:"limit"`  // 限制返回行数,0 = 默认 1000
	Offset int    `json:"offset"` // 跳过行数,默认 0
}

const (
	defaultQueryLimit = 1000
	maxQueryLimit     = 10000
)

func (h *DatabaseHandler) ExecuteQuery(c *gin.Context) {
	id := c.Param("id")
	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	start := time.Now()
	log.Printf("ExecuteQuery: db.Type=%s, db.Password=%s, query=%s", db.Type, db.Password, req.Query)
	result, err := executeQuery(&db, req.Query)
	duration := time.Since(start).Milliseconds()
	log.Printf("ExecuteQuery result: %v, error: %v", result, err)

	queryLog := model.DatabaseQuery{
		DatabaseID: db.ID,
		Query:      req.Query,
		Duration:   duration,
		ExecutedAt: time.Now(),
	}

	if err != nil {
		queryLog.Error = err.Error()
		database.DB.Create(&queryLog)
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "duration": duration})
		return
	}

	// 性能优化:服务端分页(limit/offset),防止几万行查询让前端卡死
	totalRows := len(result)
	limit := req.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	paged := result
	if offset >= totalRows {
		paged = []map[string]interface{}{}
	} else {
		end := offset + limit
		if end > totalRows {
			end = totalRows
		}
		paged = result[offset:end]
	}

	queryLog.Rows = totalRows
	database.DB.Create(&queryLog)

	c.JSON(http.StatusOK, gin.H{
		"result":     paged,
		"duration":   duration,
		"rows":       len(paged),
		"total_rows": totalRows,
		"limit":      limit,
		"offset":     offset,
		"has_more":   offset+len(paged) < totalRows,
	})
}

func executeQuery(db *model.Database, query string) ([]map[string]interface{}, error) {
	// Kafka 处理
	if db.Type == "kafka" {
		return executeKafkaQuery(db, query)
	}

	// Redis 处理
	if db.Type == "redis" {
		rdb := getRedisClient(db)
		defer rdb.Close()

		// 解析 Redis 命令
		args := strings.Fields(query)
		if len(args) == 0 {
			return nil, fmt.Errorf("empty query")
		}

		cmd := strings.ToUpper(args[0])

		// 转换 args 从 []string 到 []interface{}
		var cmdArgs []interface{}
		for _, arg := range args {
			cmdArgs = append(cmdArgs, arg)
		}

		switch cmd {
		case "KEYS":
			return nil, fmt.Errorf("KEYS 命令会阻塞 Redis，请使用 SCAN 命令代替")
		case "SCAN":
			// SCAN 命令处理，默认返回10个key，带值
			// 支持 SCAN 0 10 或 SCAN 0
			pattern := "*"
			count := 10
			if len(args) > 1 {
				// 第一个参数可能是 cursor 或 pattern
				if args[1] == "0" || args[1] == "" {
					// cursor 为 0，从头开始
				} else {
					pattern = args[1]
				}
			}
			if len(args) > 2 {
				// 第三个参数是 count
				if n, err := strconv.Atoi(args[2]); err == nil {
					count = n
				} else {
					// 可能是 pattern
					pattern = args[2]
				}
			}
			if len(args) > 3 {
				if n, err := strconv.Atoi(args[3]); err == nil {
					count = n
				}
			}
			var results []map[string]interface{}
			cursor := uint64(0)
			for len(results) < count {
				keys, nextCursor, err := rdb.Scan(context.Background(), cursor, pattern, int64(count)).Result()
				if err != nil {
					return nil, err
				}
				for _, key := range keys {
					if len(results) >= count {
						break
					}
					typ, _ := rdb.Type(context.Background(), key).Result()
					// 获取 key 的值
					var value string
					switch typ {
					case "string":
						val, _ := rdb.Get(context.Background(), key).Result()
						value = val
					case "list":
						val, _ := rdb.LRange(context.Background(), key, 0, 9).Result()
						value = fmt.Sprintf("%v", val)
					case "hash":
						val, _ := rdb.HGetAll(context.Background(), key).Result()
						value = fmt.Sprintf("%v", val)
					case "set":
						val, _ := rdb.SMembers(context.Background(), key).Result()
						value = fmt.Sprintf("%v", val)
					case "zset":
						val, _ := rdb.ZRangeWithScores(context.Background(), key, 0, 9).Result()
						value = fmt.Sprintf("%v", val)
					}
					results = append(results, map[string]interface{}{"key": key, "type": typ, "value": value})
				}
				cursor = nextCursor
				if cursor == 0 {
					break
				}
			}
			return results, nil
		case "INFO":
			result, err := rdb.Do(context.Background(), cmdArgs...).Text()
			if err != nil {
				return nil, err
			}
			// INFO 返回多行，按行拆分
			var results []map[string]interface{}
			lines := strings.Split(result, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					results = append(results, map[string]interface{}{"info": line})
				}
			}
			return results, nil
		case "GET":
			// GET 返回字符串
			result, err := rdb.Do(context.Background(), cmdArgs...).Text()
			if err != nil {
				// 如果 key 不存在，返回空
				if err.Error() == "redis: nil" {
					return []map[string]interface{}{{"result": "(nil)"}}, nil
				}
				return nil, err
			}
			return []map[string]interface{}{{"result": result}}, nil
		case "HGETALL", "LRANGE", "SMEMBERS", "ZRANGE", "MGET", "HGET", "HMGET", "TYPE", "TTL", "EXISTS", "DBSIZE":
			result, err := rdb.Do(context.Background(), cmdArgs...).Slice()
			if err != nil {
				return nil, err
			}

			var results []map[string]interface{}
			if len(result) > 0 {
				results = append(results, map[string]interface{}{"result": fmt.Sprintf("%v", result)})
			}
			return results, nil
		default:
			result, err := rdb.Do(context.Background(), cmdArgs...).Result()
			if err != nil {
				return nil, err
			}
			return []map[string]interface{}{{"result": fmt.Sprintf("%v", result)}}, nil
		}
	}

	sqlDB, err := getSQLDBWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	query = strings.TrimSpace(query)
	lowerQuery := strings.ToLower(query)
	isSelect := strings.HasPrefix(lowerQuery, "select")
	isShow := strings.HasPrefix(lowerQuery, "show")
	isDescribe := strings.HasPrefix(lowerQuery, "describe") || strings.HasPrefix(lowerQuery, "desc")

	if !isSelect && !isShow && !isDescribe {
		result, err := sqlDB.Exec(query)
		if err != nil {
			return nil, err
		}
		rowsAffected, _ := result.RowsAffected()
		return []map[string]interface{}{{"affected_rows": rowsAffected}}, nil
	}

	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			if v, ok := values[i].([]byte); ok {
				row[col] = string(v)
			} else {
				row[col] = values[i]
			}
		}
		results = append(results, row)
	}

	return results, nil
}

func (h *DatabaseHandler) GetDatabaseGroups(c *gin.Context) {
	var groups []string
	database.DB.Model(&model.Database{}).Distinct("`group`").Pluck("`group`", &groups)
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

func (h *DatabaseHandler) GetSchemas(c *gin.Context) {
	id := c.Param("id")
	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	log.Printf("GetSchemas: db.ShowAllSchemas = %v, db.Database = %s, type = %s, password = '***'", db.ShowAllSchemas, db.Database, db.Type)

	// Redis 总是返回数据库列表
	if db.Type == "redis" {
		schemas, err := getSchemas(&db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"schemas": schemas})
		return
	}

	// Kafka 返回 topics 列表
	if db.Type == "kafka" {
		schemas, err := getKafkaSchemas(&db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"schemas": schemas})
		return
	}

	// 如果没有启用显示全部模式，返回配置的数据库作为默认
	if !db.ShowAllSchemas {
		schemas := []string{}
		if db.Database != "" {
			schemas = append(schemas, db.Database)
		}
		c.JSON(http.StatusOK, gin.H{"schemas": schemas})
		return
	}

	schemas, err := getSchemas(&db)
	if err != nil {
		log.Printf("getSchemas error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("getSchemas success, got %d schemas: %v", len(schemas), schemas)
	c.JSON(http.StatusOK, gin.H{"schemas": schemas})
}

func getSchemas(db *model.Database) ([]string, error) {
	log.Printf("getSchemas called for db: %s, showAllSchemas: %v, type: %s", db.Name, db.ShowAllSchemas, db.Type)

	// Redis 处理
	if db.Type == "redis" {
		rdb := getRedisClient(db)
		defer rdb.Close()

		result, err := rdb.ConfigGet(context.Background(), "databases").Result()
		if err != nil {
			log.Printf("Redis ConfigGet error: %v, using default 16 databases", err)
		}
		dbCount := 16
		log.Printf("Redis ConfigGet result: %v, type: %T", result, result)
		if val, ok := result["databases"]; ok {
			if n, err := strconv.Atoi(val); err == nil {
				dbCount = n
			}
		}
		log.Printf("Redis dbCount: %d", dbCount)

		var schemas []string
		for i := 0; i < dbCount; i++ {
			schemas = append(schemas, "db"+strconv.Itoa(i))
		}
		log.Printf("Redis schemas: %v", schemas)
		return schemas, nil
	}

	var dsn string
	switch db.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local",
			db.Username, url.QueryEscape(db.Password), db.Host, db.Port)
	case "postgresql", "postgres":
		sslmode := "disable"
		if db.SSLMode != "" {
			sslmode = db.SSLMode
		}
		escapedPassword := strings.ReplaceAll(db.Password, "'", "''")
		dsn = fmt.Sprintf("host=%s port=%d user=%s password='%s' dbname=postgres sslmode=%s",
			db.Host, db.Port, db.Username, escapedPassword, sslmode)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.Type)
	}

	log.Printf("DSN: %s", dsn)
	sqlDB, err := getSQLDBWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	var schemas []string
	switch db.Type {
	case "mysql":
		rows, err := sqlDB.Query("SHOW DATABASES")
		if err != nil {
			log.Printf("SHOW DATABASES error: %v", err)
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			if name != "information_schema" && name != "mysql" && name != "performance_schema" && name != "sys" {
				schemas = append(schemas, name)
			}
		}
		log.Printf("MySQL schemas (filtered): %v", schemas)
	case "postgresql", "postgres":
		rows, err := sqlDB.Query("SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			schemas = append(schemas, name)
		}
	}
	return schemas, nil
}

func getKafkaSchemas(db *model.Database) ([]string, error) {
	conn, err := dialKafkaWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	topicList, err := conn.ReadPartitions()
	if err != nil {
		return nil, err
	}

	topicSet := make(map[string]bool)
	for _, p := range topicList {
		topicSet[p.Topic] = true
	}

	var topics []string
	for t := range topicSet {
		topics = append(topics, t)
	}

	return topics, nil
}

func (h *DatabaseHandler) GetTables(c *gin.Context) {
	id := c.Param("id")
	schema := c.Query("schema")
	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	log.Printf("GetTables: db.Password='%s', db.Database='%s'", db.Password, db.Database)

	// Kafka 处理 - 获取 topic 的 partitions
	if db.Type == "kafka" {
		tables, err := getKafkaTables(&db, schema)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tables": tables})
		return
	}

	tables, err := getTables(&db, schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tables": tables})
}

func getTables(db *model.Database, schema string) ([]map[string]interface{}, error) {
	log.Printf("getTables: db.Name=%s, db.Host=%s, db.Password=%s, db.Type=%s, schema=%s", db.Name, db.Host, db.Password, db.Type, schema)

	// Redis 处理 - 获取 key 列表
	if db.Type == "redis" {
		rdb := getRedisClient(db)
		defer rdb.Close()

		// 切换到指定的数据库 (处理 db0, db1 等格式)
		if schema != "" {
			dbNum := 0
			// 去掉 "db" 前缀
			schemaNum := strings.TrimPrefix(schema, "db")
			if n, err := strconv.Atoi(schemaNum); err == nil {
				dbNum = n
			}
			if dbNum > 0 {
				rdb = redis.NewClient(&redis.Options{
					Addr:     fmt.Sprintf("%s:%d", db.Host, db.Port),
					Password: db.Password,
					DB:       dbNum,
				})
				defer rdb.Close()
			}
		}

		// 使用 SCAN 获取 keys，最多返回10个
		var tables []map[string]interface{}
		cursor := uint64(0)
		maxKeys := 10
		for len(tables) < maxKeys {
			keys, nextCursor, err := rdb.Scan(context.Background(), cursor, "*", 100).Result()
			if err != nil {
				log.Printf("Redis scan error: %v", err)
				return nil, err
			}
			for _, key := range keys {
				if len(tables) >= maxKeys {
					break
				}
				typ, _ := rdb.Type(context.Background(), key).Result()
				tables = append(tables, map[string]interface{}{"name": key, "type": typ})
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
			if cursor == 0 {
				break
			}
		}
		return tables, nil
	}

	log.Printf("getTables: %s:%d", db.Host, db.Port)
	sqlDB, err := getSQLDBWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	var tables []map[string]interface{}
	switch db.Type {
	case "mysql":
		dbName := schema
		if dbName == "" {
			dbName = db.Database
			if dbName == "" {
				dbName = "mysql"
			}
		}
		rows, err := sqlDB.Query("SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = ?", dbName)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, ttype string
			rows.Scan(&name, &ttype)
			tables = append(tables, map[string]interface{}{"name": name, "type": ttype})
		}
	case "postgresql", "postgres":
		schemaName := schema
		if schemaName == "" {
			schemaName = "public"
		}
		rows, err := sqlDB.Query("SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = $1", schemaName)
		if err != nil {
			log.Printf("PostgreSQL getTables error: %v", err)
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, ttype string
			rows.Scan(&name, &ttype)
			tables = append(tables, map[string]interface{}{"name": name, "type": ttype})
		}
	}
	return tables, nil
}

func getKafkaTables(db *model.Database, topic string) ([]map[string]interface{}, error) {
	conn, err := dialKafkaWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions(topic)
	if err != nil {
		return nil, err
	}

	var tables []map[string]interface{}
	seen := make(map[int]bool)
	for _, p := range partitions {
		if !seen[p.ID] {
			seen[p.ID] = true
			tables = append(tables, map[string]interface{}{
				"name":     fmt.Sprintf("partition-%d", p.ID),
				"type":     "partition",
				"leader":   p.Leader,
				"replicas": len(p.Replicas),
				"isr":      len(p.Isr),
			})
		}
	}

	return tables, nil
}

func (h *DatabaseHandler) GetColumns(c *gin.Context) {
	id := c.Param("id")
	tableName := c.Query("table")
	schema := c.Query("schema")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table is required"})
		return
	}

	var db model.Database
	if err := database.DB.First(&db, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database not found"})
		return
	}

	columns, err := getColumnsWithSchema(&db, tableName, schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"columns": columns})
}

func getColumnsWithSchema(db *model.Database, tableName string, schema string) ([]map[string]interface{}, error) {
	sqlDB, err := getSQLDBWithProxy(db)
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	var columns []map[string]interface{}
	switch db.Type {
	case "mysql":
		rows, err := sqlDB.Query("DESCRIBE `" + tableName + "`")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var field, dtype, nullable, key, def, extra string
			rows.Scan(&field, &dtype, &nullable, &key, &def, &extra)
			columns = append(columns, map[string]interface{}{
				"name":     field,
				"type":     dtype,
				"nullable": nullable,
				"key":      key,
				"default":  def,
			})
		}
	case "postgresql", "postgres":
		schemaName := schema
		if schemaName == "" {
			schemaName = "public"
		}
		rows, err := sqlDB.Query(`
			SELECT column_name, data_type, is_nullable, column_default
			FROM information_schema.columns 
			WHERE table_schema = $1 AND table_name = $2
			ORDER BY ordinal_position
		`, schemaName, tableName)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, dtype, nullable, def string
			rows.Scan(&name, &dtype, &nullable, &def)
			columns = append(columns, map[string]interface{}{
				"name":     name,
				"type":     dtype,
				"nullable": nullable,
				"default":  def,
			})
		}
	}
	return columns, nil
}

func getColumns(db *model.Database, tableName string) ([]map[string]interface{}, error) {
	dsn := getDSN(db)
	var sqlDB *sql.DB
	var err error

	switch db.Type {
	case "mysql":
		sqlDB, err = sql.Open("mysql", dsn)
	case "postgresql", "postgres":
		sqlDB, err = sql.Open("postgres", dsn)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.Type)
	}

	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	var columns []map[string]interface{}
	switch db.Type {
	case "mysql":
		rows, err := sqlDB.Query("DESCRIBE `" + tableName + "`")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var field, dtype, nullable, key, def, extra string
			rows.Scan(&field, &dtype, &nullable, &key, &def, &extra)
			columns = append(columns, map[string]interface{}{
				"name":     field,
				"type":     dtype,
				"nullable": nullable,
				"key":      key,
				"default":  def,
			})
		}
	case "postgresql", "postgres":
		rows, err := sqlDB.Query(`
			SELECT column_name, data_type, is_nullable, column_default
			FROM information_schema.columns 
			WHERE table_schema = 'public' AND table_name = ?
			ORDER BY ordinal_position
		`, tableName)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, dtype, nullable, def any
			rows.Scan(&name, &dtype, &nullable, &def)
			columns = append(columns, map[string]interface{}{
				"name":     name,
				"type":     dtype,
				"nullable": nullable,
				"default":  def,
			})
		}
	}
	return columns, nil
}
