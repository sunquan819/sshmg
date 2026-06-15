package main

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"deploy-manager/internal/config"
	"deploy-manager/internal/database"
	"deploy-manager/internal/handler"
	"deploy-manager/internal/middleware"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

//go:embed web
var webAssets embed.FS

//go:embed rdp-agent.exe
var rdpAgentExe []byte

//go:embed default_config.yaml
var defaultConfigFile []byte

//go:embed default_scenarios.json
var defaultScenariosJSON []byte

//go:embed default_templates.json
var defaultTemplatesJSON []byte

func getAppDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

func main() {
	configPath := "config.yaml"
	port := 0
	password := ""
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("Usage: deploy-manager [config.yaml]")
			fmt.Println("   or: deploy-manager --port 3001")
			fmt.Println("   or: deploy-manager --port 3001 --password yourpass")
			fmt.Println("   or: deploy-manager -p 3001 -P yourpass")
			os.Exit(0)
		}
		i := 1
		for i < len(os.Args) {
			arg := os.Args[i]
			if arg == "-port" || arg == "--port" || arg == "-p" {
				if i+1 < len(os.Args) {
					fmt.Sscanf(os.Args[i+1], "%d", &port)
					i += 2
				}
			} else if arg == "-password" || arg == "--password" || arg == "-P" {
				if i+1 < len(os.Args) {
					password = os.Args[i+1]
					i += 2
				}
			} else {
				configPath = os.Args[i]
				i++
			}
		}
	}

	appDir := getAppDir()
	if configPath == "config.yaml" {
		configPath = filepath.Join(appDir, configPath)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := createDefaultConfig(configPath); err != nil {
			log.Fatalf("Failed to create default config: %v", err)
		}
		log.Printf("Default config created at %s", configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if port > 0 {
		cfg.Server.Port = port
	}

	if password != "" {
		cfg.Admin.Password = password
	}

	service.SSHSvc.SetIdleTTL(time.Duration(cfg.SSH.EffectiveIdleTTL()) * time.Minute)

	if err := database.Init(config.GetDBPath()); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	if err := initAdminUser(cfg); err != nil {
		log.Fatalf("Failed to initialize admin user: %v", err)
	}

	resetScenarios := false
	for i, arg := range os.Args {
		if (arg == "--reset-scenarios" || arg == "-r") && i+1 < len(os.Args) && os.Args[i+1] == "true" {
			resetScenarios = true
		}
	}

	if err := initDefaultScenarios(resetScenarios); err != nil {
		log.Printf("Warning: Failed to initialize default scenarios: %v", err)
	}

	if err := service.DockerSvc.Init(); err != nil {
		log.Printf("Warning: Docker service initialization failed: %v", err)
	}

	r := gin.New()
	// 替代 gin.Default() 自带的 logger + recovery,加上自定义的 panic recover middleware
	r.Use(gin.Logger())
	r.Use(middleware.Recovery())

	tmpl := template.Must(template.ParseFS(webAssets, "web/templates/*.html"))
	r.SetHTMLTemplate(tmpl)

	// HTML 页面和静态资源都加 no-cache 头，避免 embed 模板改了之后浏览器还显示旧版
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") ||
			c.Request.URL.Path == "/" ||
			strings.HasSuffix(c.Request.URL.Path, ".html") ||
			!strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	})

	r.GET("/static/*filepath", func(c *gin.Context) {
		filePath := c.Param("filepath")
		content, err := webAssets.ReadFile("web/static" + filePath)
		if err != nil {
			c.Status(404)
			return
		}
		ext := filepath.Ext(filePath)
		contentType := "text/plain"
		switch ext {
		case ".css":
			contentType = "text/css"
		case ".js":
			contentType = "application/javascript"
		case ".html":
			contentType = "text/html"
		}
		c.Data(200, contentType, content)
	})

	r.GET("/artifacts/*filepath", func(c *gin.Context) {
		filePath := c.Param("filepath")

		if filePath == "/rdp-agent.exe" && len(rdpAgentExe) > 0 {
			c.Header("Content-Disposition", "attachment")
			c.Data(200, "application/x-msdownload", rdpAgentExe)
			return
		}

		fullPath := "../artifacts" + filePath
		log.Printf("Serving artifact: %s", fullPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			c.Status(404)
			return
		}
		ext := filepath.Ext(filePath)
		contentType := "application/octet-stream"
		switch ext {
		case ".exe":
			contentType = "application/x-msdownload"
		}
		c.Header("Content-Disposition", "attachment")
		c.Data(200, contentType, content)
	})

	authHandler := handler.NewAuthHandler()
	serverHandler := handler.NewServerHandler()
	deployHandler := handler.NewDeployHandler()
	cronHandler := handler.NewCronHandler()
	fileHandler := handler.NewFileHandler()
	terminalHandler := handler.NewTerminalHandler()
	infrastructureHandler := handler.NewInfrastructureHandler()
	templateHandler := handler.NewTemplateHandler()
	databaseHandler := &handler.DatabaseHandler{}
	containerHandler := &handler.ContainerHandler{}
	projectHandler := &handler.ProjectHandler{}
	noteHandler := handler.NewNoteHandler()
	toolHandler := handler.NewToolHandler()
	tunnelHandler := handler.NewTunnelHandler()
	commandHandler := handler.NewCommandHandler()
	terminalLogHandler := handler.NewTerminalLogHandler()
	localFilesHandler := handler.NewLocalFilesHandler()

	go handler.InitDefaultProject()
	go commandHandler.InitDefaultCommands()

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", nil)
	})

	r.GET("/terminal", func(c *gin.Context) {
		log.Println("Serving /terminal page")
		c.HTML(http.StatusOK, "terminal.html", nil)
	})

	r.GET("/containers", func(c *gin.Context) {
		c.HTML(http.StatusOK, "containers.html", nil)
	})

	r.GET("/container-terminal", func(c *gin.Context) {
		log.Println("Serving /container-terminal page")
		c.HTML(http.StatusOK, "container-terminal.html", nil)
	})

	r.GET("/infrastructure", func(c *gin.Context) {
		log.Println("Serving /infrastructure page")
		c.HTML(http.StatusOK, "infrastructure.html", nil)
	})

	r.GET("/database", func(c *gin.Context) {
		log.Println("Serving /database page")
		c.HTML(http.StatusOK, "database.html", nil)
	})

	r.GET("/projects", func(c *gin.Context) {
		log.Println("Serving /projects page")
		c.HTML(http.StatusOK, "projects.html", nil)
	})

	r.GET("/notes", func(c *gin.Context) {
		log.Println("Serving /notes page")
		c.HTML(http.StatusOK, "notes.html", nil)
	})

	r.GET("/tools", func(c *gin.Context) {
		log.Println("Serving /tools page")
		c.HTML(http.StatusOK, "tools.html", nil)
	})

r.GET("/tunnels", func(c *gin.Context) {
		c.HTML(http.StatusOK, "tunnels.html", nil)
	})

	r.GET("/terminal-logs", func(c *gin.Context) {
		c.HTML(http.StatusOK, "terminal-logs.html", nil)
	})

	r.GET("/files", func(c *gin.Context) {
		c.HTML(http.StatusOK, "files.html", nil)
	})

	api := r.Group("/api")
	{
		api.POST("/login", authHandler.Login)
		api.POST("/refresh", authHandler.RefreshToken)
		api.POST("/verify-admin", authHandler.VerifyAdminPassword)

		auth := api.Group("")
		auth.Use(middleware.JWTAuth())
		{
			auth.GET("/user", authHandler.GetUserInfo)
			auth.POST("/change-password", authHandler.ChangePassword)
			auth.POST("/logout", authHandler.Logout)

			servers := auth.Group("/servers")
			{
				servers.GET("", serverHandler.ListServers)
				servers.GET("/:id", serverHandler.GetServer)
				servers.GET("/:id/full", serverHandler.GetServerFull)
				servers.POST("", serverHandler.CreateServer)
				servers.PUT("/:id", serverHandler.UpdateServer)
				servers.DELETE("/:id", serverHandler.DeleteServer)
				servers.GET("/:id/test", serverHandler.TestConnection)
				servers.POST("/:id/exec", serverHandler.ExecCommand)
				servers.GET("/:id/cron", cronHandler.ListServerCronJobs)
				servers.POST("/check-all", serverHandler.CheckAllServers)
				servers.GET("/groups", serverHandler.GetServerGroups)
			}

			deployments := auth.Group("/deployments")
			{
				deployments.GET("", deployHandler.ListDeployments)
				deployments.GET("/:id", deployHandler.GetDeployment)
				deployments.POST("", deployHandler.CreateDeployment)
				deployments.DELETE("/:id", deployHandler.DeleteDeployment)
				deployments.POST("/:id/restart", deployHandler.RestartDeployment)
				deployments.GET("/:id/logs", deployHandler.GetDeploymentLogs)
			}

			crons := auth.Group("/crons")
			{
				crons.GET("", cronHandler.ListCronJobs)
				crons.GET("/:id", cronHandler.GetCronJob)
				crons.POST("", cronHandler.CreateCronJob)
				crons.PUT("/:id", cronHandler.UpdateCronJob)
				crons.DELETE("/:id", cronHandler.DeleteCronJob)
				crons.POST("/:id/execute", cronHandler.ExecuteCronJob)
				crons.GET("/history", cronHandler.ListCronHistory)
			}

			files := auth.Group("/files")
			{
				files.GET("/:id", fileHandler.ListFiles)
				files.GET("/:id/read", fileHandler.ReadFile)
				files.PUT("/:id/write", fileHandler.WriteFile)
				files.POST("/:id/write", fileHandler.WriteFile)
				files.POST("/:id/upload", fileHandler.UploadFile)
				files.GET("/:id/download", fileHandler.DownloadFile)
				files.DELETE("/:id", fileHandler.DeleteFile)
				files.POST("/:id/mkdir", fileHandler.CreateDirectory)
				files.PUT("/:id/move", fileHandler.MoveFile)
				files.GET("/:id/search", fileHandler.SearchFiles)
			}

			terminal := auth.Group("/terminal")
			{
				terminal.GET("/:id", terminalHandler.Connect)
				terminal.DELETE("/:id", terminalHandler.Disconnect)
				terminal.GET("", terminalHandler.ListSessions)
			}

			rdp := auth.Group("/rdp")
			{
				rdp.POST("/:id/connect", terminalHandler.ConnectRDP)
				rdp.POST("/:id/disconnect", terminalHandler.DisconnectRDP)
			}

			containers := auth.Group("/containers")
			{
				containers.GET("", containerHandler.ListContainers)
				containers.GET("/:id", containerHandler.GetContainerInfo)
				containers.GET("/:id/logs", containerHandler.GetContainerLogs)
				containers.GET("/:id/stats", containerHandler.GetContainerStats)
				containers.POST("/:id/start", containerHandler.StartContainer)
				containers.POST("/:id/stop", containerHandler.StopContainer)
				containers.POST("/:id/restart", containerHandler.RestartContainer)
				containers.DELETE("/:id", containerHandler.RemoveContainer)
				containers.POST("/:id/exec", containerHandler.ExecContainer)
				containers.GET("/images", containerHandler.ListImages)
				containers.GET("/check-docker", containerHandler.CheckDocker)
			}

			projects := auth.Group("/projects")
			{
				projects.GET("", projectHandler.ListProjects)
				projects.GET("/:id", projectHandler.GetProject)
				projects.POST("", projectHandler.CreateProject)
				projects.PUT("/:id", projectHandler.UpdateProject)
				projects.DELETE("/:id", projectHandler.DeleteProject)

				projects.POST("/components", projectHandler.CreateComponent)
				projects.PUT("/components/:id", projectHandler.UpdateComponent)
				projects.DELETE("/components/:id", projectHandler.DeleteComponent)
				projects.POST("/components/:id/deploy", projectHandler.DeployComponent)
				projects.POST("/components/:id/deploy-update", projectHandler.DeployUpdate)
				projects.GET("/components/:id/deploy-status", projectHandler.GetDeployStatus)
				projects.GET("/components/:id/check-running", projectHandler.CheckRunning)
				projects.POST("/components/:id/package", projectHandler.UploadPackage)
				projects.POST("/components/:id/package/copy", projectHandler.CopyPackageFile)
				projects.DELETE("/components/:id/package", projectHandler.DeletePackage)
				projects.POST("/components/:id/action", projectHandler.ComponentAction)
				projects.POST("/components/:id/fetch-version", projectHandler.FetchVersion)
				projects.GET("/components/:id/log", projectHandler.GetDeployLog)
			}

			notes := auth.Group("/notes")
			{
				notes.GET("", noteHandler.ListNotes)
				notes.GET("/:id", noteHandler.GetNote)
				notes.POST("", noteHandler.CreateNote)
				notes.PUT("/:id", noteHandler.UpdateNote)
				notes.DELETE("/:id", noteHandler.DeleteNote)
			}

			commands := auth.Group("/commands")
			{
				commands.GET("", commandHandler.ListCommands)
				commands.POST("", commandHandler.CreateCommand)
				commands.PUT("/:id", commandHandler.UpdateCommand)
				commands.DELETE("/:id", commandHandler.DeleteCommand)
			}

			tools := auth.Group("/tools")
			{
				tools.POST("/exec", toolHandler.ExecTool)
			}

			tunnels := auth.Group("/tunnels")
			{
				tunnels.GET("", tunnelHandler.ListTunnels)
				tunnels.POST("", tunnelHandler.StartTunnel)
				tunnels.GET("/:id", tunnelHandler.GetTunnel)
				tunnels.DELETE("/:id", tunnelHandler.StopTunnel)
			}

			infrastructure := auth.Group("/infrastructure")
			{
				infrastructure.GET("/scenarios", infrastructureHandler.ListScenarios)
				infrastructure.GET("/scenarios/:id", infrastructureHandler.GetScenario)
				infrastructure.POST("/scenarios", infrastructureHandler.CreateScenario)
				infrastructure.PUT("/scenarios/:id", infrastructureHandler.UpdateScenario)
				infrastructure.DELETE("/scenarios/:id", infrastructureHandler.DeleteScenario)
				infrastructure.POST("/scenarios/:id/files", infrastructureHandler.UploadScenarioFiles)
				infrastructure.DELETE("/scenarios/:id/files/:filename", infrastructureHandler.DeleteScenarioFile)
				infrastructure.GET("/scenarios/:id/files/:filename", infrastructureHandler.GetScenarioFile)
				infrastructure.PUT("/scenarios/:id/files/:filename", infrastructureHandler.UpdateScenarioFile)
				infrastructure.POST("/execute", infrastructureHandler.ExecuteScenario)
				infrastructure.GET("/executions", infrastructureHandler.ListExecutions)
				infrastructure.GET("/executions/:id", infrastructureHandler.GetExecution)

				// Docker Compose ???????????
				infrastructure.GET("/templates", templateHandler.ListTemplates)
				infrastructure.GET("/templates/:id", templateHandler.GetTemplate)
				infrastructure.POST("/templates", templateHandler.CreateTemplate)
				infrastructure.PUT("/templates/:id", templateHandler.UpdateTemplate)
				infrastructure.DELETE("/templates/:id", templateHandler.DeleteTemplate)
				infrastructure.POST("/templates/validate", templateHandler.ValidateContent)
			}

			databases := auth.Group("/databases")
			{
				databases.GET("", databaseHandler.ListDatabases)
				databases.GET("/:id", databaseHandler.GetDatabase)
				databases.POST("", databaseHandler.CreateDatabase)
				databases.PUT("/:id", databaseHandler.UpdateDatabase)
				databases.DELETE("/:id", databaseHandler.DeleteDatabase)
				databases.GET("/:id/test", databaseHandler.TestConnection)
				databases.POST("/test", databaseHandler.TestConnectionDirect)
				databases.POST("/:id/query", databaseHandler.ExecuteQuery)
				databases.GET("/groups", databaseHandler.GetDatabaseGroups)
				databases.GET("/:id/schemas", databaseHandler.GetSchemas)
				databases.GET("/:id/tables", databaseHandler.GetTables)
				databases.GET("/:id/columns", databaseHandler.GetColumns)
			}

			terminalLogs := auth.Group("/terminal-logs")
			{
				terminalLogs.GET("", terminalLogHandler.ListTerminalLogs)
				terminalLogs.GET("/:id", terminalLogHandler.GetTerminalLog)
				terminalLogs.POST("", terminalLogHandler.CreateTerminalLog)
				terminalLogs.DELETE("/:id", terminalLogHandler.DeleteTerminalLog)
				terminalLogs.DELETE("", terminalLogHandler.DeleteTerminalLogs)
				terminalLogs.GET("/stats", terminalLogHandler.GetServerStats)
				terminalLogs.DELETE("/clear", terminalLogHandler.ClearOldLogs)
			}

			handler.RegisterLocalFilesRoutes(auth, localFilesHandler)
		}

		// WebSocket ????????auth ?????????handler ?????????token
		api.GET("/ws/terminal/:id", terminalHandler.Connect)
		api.GET("/ws/container-terminal/:id/:containerId", terminalHandler.ConnectContainerTerminal)

		// ???????????
		containerFiles := auth.Group("/container-files")
		{
			containerFiles.GET("/:containerId/list", terminalHandler.ListContainerFiles)
			containerFiles.GET("/:containerId/read", terminalHandler.ReadContainerFile)
			containerFiles.PUT("/:containerId/write", terminalHandler.WriteContainerFile)
			containerFiles.POST("/:containerId/upload", terminalHandler.UploadContainerFile)
			containerFiles.GET("/:containerId/download", terminalHandler.DownloadContainerFile)
			containerFiles.POST("/:containerId/mkdir", terminalHandler.CreateContainerDir)
			containerFiles.DELETE("/:containerId", terminalHandler.DeleteContainerFile)
		}
	}

	// ?????????????? WebSocket ????
	r.GET("/ws/terminal/:id", terminalHandler.Connect)
	r.GET("/ws/container-terminal/:id/:containerId", terminalHandler.ConnectContainerTerminal)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
	log.Printf("Starting server on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func createDefaultConfig(path string) error {
	return os.WriteFile(path, defaultConfigFile, 0644)
}

func initAdminUser(cfg *config.Config) error {
	var user model.User
	if err := database.DB.Where("username = ?", cfg.Admin.Username).First(&user).Error; err == nil {
		if cfg.Admin.Password != "" {
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(cfg.Admin.Password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			user.Password = string(hashedPassword)
			database.DB.Save(&user)
			log.Printf("Admin password updated via command line")
		}
		return nil
	}

	password := cfg.Admin.Password
	generateNewPassword := false
	if password == "" {
		generateNewPassword = true
		password = generateRandomPassword(16)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	adminUser := model.User{
		Username: cfg.Admin.Username,
		Password: string(hashedPassword),
		Role:     "admin",
	}

	if err := database.DB.Create(&adminUser).Error; err != nil {
		return err
	}

	if generateNewPassword {
		log.Println("=================================================")
		log.Printf("Admin user created!")
		log.Printf("Username: %s", cfg.Admin.Username)
		log.Println("Password: [Generated and set - check startup logs]")
		log.Println("=================================================")
		log.Println("Please change the password after first login!")
	}

	return nil
}

func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

type defaultScenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Playbook    string `json:"playbook"`
}

func initDefaultScenarios(reset bool) error {
	var count int64
	database.DB.Model(&model.InfrastructureScenario{}).Count(&count)
	if count > 0 && !reset {
		return nil
	}

	if reset {
		database.DB.Exec("DELETE FROM infrastructure_scenarios")
		log.Printf("Resetting default scenarios")
	}

	var scenarios []defaultScenario
	if err := json.Unmarshal(defaultScenariosJSON, &scenarios); err != nil {
		return err
	}

	for _, s := range scenarios {
		scenario := model.InfrastructureScenario{
			Name:        s.Name,
			Description: s.Description,
			Playbook:    s.Playbook,
		}
		database.DB.Create(&scenario)
		log.Printf("Created default scenario: %s", s.Name)
	}

	return nil
}
