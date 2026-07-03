# Desktop Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Wails v2 desktop GUI wrapping the existing web app, with RDP integration, while keeping the standalone web server working.

**Architecture:** Wails webview loads from embedded Gin server on random localhost port. All existing frontend (Alpine.js, templates, xterm.js, CodeMirror) and backend (Gin handlers, services) code is reused unchanged. RDP agent logic extracted to shared package.

**Tech Stack:** Go 1.23, Wails v2, Gin, embed.FS

## Global Constraints

- Web version (`go build ./cmd/server`) must still produce a working standalone binary
- Desktop webview loads from `http://127.0.0.1:<random-port>` (Gin server), not from Wails asset server
- Must build on Windows, macOS, Linux
- Wails v2 only — not v3 or WebView2 directly
- CGO required for desktop build only; web build stays CGO_ENABLED=0
- Frontend files unchanged except for RDP launch path (2-line addition)

---

### Task 1: Create `pkg/assets/` — shared embedded assets

**Files:**
- Create: `pkg/assets/assets.go`
- Move: `cmd/server/web/` → `pkg/assets/web/`
- Move: `cmd/server/rdp-agent.exe` → `pkg/assets/rdp-agent.exe`
- Move: `cmd/server/default_config.yaml` → `pkg/assets/default_config.yaml`
- Move: `cmd/server/default_scenarios.json` → `pkg/assets/default_scenarios.json`
- Move: `cmd/server/default_templates.json` → `pkg/assets/default_templates.json`
- Modify: `cmd/server/main.go` — rewire imports to use `pkg/assets`

**Interfaces:**
- Produces: `assets.WebAssets embed.FS`, `assets.RDPAgentExe []byte`, `assets.DefaultConfig []byte`, `assets.DefaultScenarios []byte`, `assets.DefaultTemplates []byte`

- [ ] **Step 1: Create `pkg/assets/` directory**

```bash
mkdir pkg\assets
```

- [ ] **Step 2: Create `pkg/assets/assets.go` with embed directives**

```go
package assets

import "embed"

//go:embed web
var WebAssets embed.FS

//go:embed rdp-agent.exe
var RDPAgentExe []byte

//go:embed default_config.yaml
var DefaultConfig []byte

//go:embed default_scenarios.json
var DefaultScenarios []byte

//go:embed default_templates.json
var DefaultTemplates []byte
```

- [ ] **Step 3: Move files from `cmd/server/` to `pkg/assets/`**

```bash
Move-Item -Path "cmd/server/web" -Destination "pkg/assets/web"
Move-Item -Path "cmd/server/rdp-agent.exe" -Destination "pkg/assets/rdp-agent.exe"
Move-Item -Path "cmd/server/default_config.yaml" -Destination "pkg/assets/default_config.yaml"
Move-Item -Path "cmd/server/default_scenarios.json" -Destination "pkg/assets/default_scenarios.json"
Move-Item -Path "cmd/server/default_templates.json" -Destination "pkg/assets/default_templates.json"
```

- [ ] **Step 4: Update `cmd/server/main.go` to import from `pkg/assets`**

Replace the 5 `//go:embed` blocks and `var` declarations with imports:

Remove:
```go
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
```

Add import:
```go
import "deploy-manager/pkg/assets"
```

Replace all uses:
- `webAssets` → `assets.WebAssets`
- `rdpAgentExe` → `assets.RDPAgentExe`
- `defaultConfigFile` → `assets.DefaultConfig`
- `defaultScenariosJSON` → `assets.DefaultScenarios`
- `defaultTemplatesJSON` → `assets.DefaultTemplates`

Also remove the `"embed"` import if it's no longer used in main.go after this change (it may still be needed if main.go has any remaining `//go:embed`).

- [ ] **Step 5: Update template path in server setup**

The server setup in main.go has:
```go
tmpl := template.Must(template.ParseFS(webAssets, "web/templates/*.html"))
```

After the move, the `web/` directory is at `pkg/assets/web/`. The embed root in assets.go is `pkg/assets/`, so the path is still `"web/templates/*.html"`. No change needed.

Similarly for static file serving:
```go
content, err := webAssets.ReadFile("web/static" + filePath)
```
No change needed — same relative path.

- [ ] **Step 6: Build and verify web version still works**

```bash
go build -o deploy-manager.exe ./cmd/server/main.go
```

Expected: build succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add pkg/assets/ cmd/server/main.go
git rm -r cmd/server/web cmd/server/rdp-agent.exe cmd/server/default_*.yaml cmd/server/default_*.json
git commit -m "refactor: extract embedded assets to pkg/assets"
```

---

### Task 2: Create `pkg/server/` — extract server core

**Files:**
- Create: `pkg/server/options.go`
- Create: `pkg/server/server.go`
- Modify: `cmd/server/main.go` — thin wrapper calling `pkg/server.Start()`

**Interfaces:**
- Produces: `server.Options struct`, `server.Start(ctx, opts) (port int, err error)`
- Consumes: `assets.WebAssets`, `assets.RDPAgentExe`, `assets.DefaultConfig`, etc.

- [ ] **Step 1: Create `pkg/server/options.go`**

```go
package server

import (
	"context"
	"embed"
)

type Options struct {
	Port            int    // 0 = use config file port
	Password        string // empty = use config file password
	ConfigPath      string // default "config.yaml"
	Desktop         bool   // true = desktop mode (suppress console logs, enable RDP auto-launch)

	WebAssets       embed.FS
	RDPAgentExe     []byte
	DefaultConfig   []byte
	DefaultScenarios []byte
	DefaultTemplates []byte
}
```

- [ ] **Step 2: Create `pkg/server/server.go`**

This contains the server initialization and startup logic extracted from `cmd/server/main.go`. The core sequence is:

```go
package server

import (
	"context"
	"crypto/rand"
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

func Start(ctx context.Context, opts Options) (int, error) {
	appDir := getAppDir()
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = "config.yaml"
	}
	if configPath == "config.yaml" {
		configPath = filepath.Join(appDir, configPath)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, opts.DefaultConfig, 0644); err != nil {
			return 0, fmt.Errorf("failed to create default config: %w", err)
		}
		log.Printf("Default config created at %s", configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return 0, fmt.Errorf("failed to load config: %w", err)
	}

	if opts.Port > 0 {
		cfg.Server.Port = opts.Port
	}

	if opts.Password != "" {
		cfg.Admin.Password = opts.Password
	}

	service.SSHSvc.SetIdleTTL(time.Duration(cfg.SSH.EffectiveIdleTTL()) * time.Minute)

	if err := database.Init(config.GetDBPath()); err != nil {
		return 0, fmt.Errorf("failed to init database: %w", err)
	}
	defer database.Close()

	if err := initAdminUser(cfg); err != nil {
		return 0, fmt.Errorf("failed to init admin user: %w", err)
	}

	if err := initDefaultScenarios(opts.DefaultScenarios); err != nil {
		log.Printf("Warning: failed to init scenarios: %v", err)
	}

	if err := service.DockerSvc.Init(); err != nil {
		log.Printf("Warning: Docker init failed: %v", err)
	}

	if opts.Desktop {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(middleware.Recovery())

	tmpl := template.Must(template.ParseFS(opts.WebAssets, "web/templates/*.html"))
	r.SetHTMLTemplate(tmpl)

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
		content, err := opts.WebAssets.ReadFile("web/static" + filePath)
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
		if filePath == "/rdp-agent.exe" && len(opts.RDPAgentExe) > 0 {
			c.Header("Content-Disposition", "attachment")
			c.Data(200, "application/x-msdownload", opts.RDPAgentExe)
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

	// --- Handler instantiation (same as current main.go) ---
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

	// --- Routes (same as current main.go) ---
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})
	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", nil)
	})
	r.GET("/terminal", func(c *gin.Context) {
		c.HTML(http.StatusOK, "terminal.html", nil)
	})
	r.GET("/containers", func(c *gin.Context) {
		c.HTML(http.StatusOK, "containers.html", nil)
	})
	r.GET("/container-terminal", func(c *gin.Context) {
		c.HTML(http.StatusOK, "container-terminal.html", nil)
	})
	r.GET("/infrastructure", func(c *gin.Context) {
		c.HTML(http.StatusOK, "infrastructure.html", nil)
	})
	r.GET("/database", func(c *gin.Context) {
		c.HTML(http.StatusOK, "database.html", nil)
	})
	r.GET("/projects", func(c *gin.Context) {
		c.HTML(http.StatusOK, "projects.html", nil)
	})
	r.GET("/notes", func(c *gin.Context) {
		c.HTML(http.StatusOK, "notes.html", nil)
	})
	r.GET("/tools", func(c *gin.Context) {
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

		api.GET("/ws/terminal/:id", terminalHandler.Connect)
		api.GET("/ws/container-terminal/:id/:containerId", terminalHandler.ConnectContainerTerminal)

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

	r.GET("/ws/terminal/:id", terminalHandler.Connect)
	r.GET("/ws/container-terminal/:id/:containerId", terminalHandler.ConnectContainerTerminal)

	// Listen
	listenAddr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
	log.Printf("Starting server on %s", listenAddr)

	if err := r.Run(listenAddr); err != nil {
		return 0, fmt.Errorf("server error: %w", err)
	}
	return cfg.Server.Port, nil
}

func getAppDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
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
		}
		return nil
	}

	password := cfg.Admin.Password
	if password == "" {
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
	return database.DB.Create(&adminUser).Error
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

func initDefaultScenarios(data []byte) error {
	var scenarios []defaultScenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		return err
	}

	var count int64
	database.DB.Model(&model.InfrastructureScenario{}).Count(&count)
	if count > 0 {
		return nil
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
```

Note: This is a direct extraction. The route registration section is identical to what's in `cmd/server/main.go`.

- [ ] **Step 3: Rewrite `cmd/server/main.go` as a thin wrapper**

```go
package main

import (
	"context"
	"fmt"
	"os"

	"deploy-manager/pkg/assets"
	"deploy-manager/pkg/server"
)

func main() {
	configPath := "config.yaml"
	port := 0
	password := ""

	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("Usage: deploy-manager [config.yaml]")
			fmt.Println("   or: deploy-manager --port 3001")
			fmt.Println("   or: deploy-manager --port 3001 --password yourpass")
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

	opts := server.Options{
		Port:              port,
		Password:          password,
		ConfigPath:        configPath,
		WebAssets:         assets.WebAssets,
		RDPAgentExe:       assets.RDPAgentExe,
		DefaultConfig:     assets.DefaultConfig,
		DefaultScenarios:  assets.DefaultScenarios,
		DefaultTemplates:  assets.DefaultTemplates,
	}

	if _, err := server.Start(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

Remove the `"embed"` import from main.go since it's no longer needed there.

- [ ] **Step 4: Build and verify web version still works**

```bash
go build -o deploy-manager.exe ./cmd/server/main.go
```

Expected: build succeeds.

- [ ] **Step 5: Commit**

```bash
git add pkg/server/ cmd/server/main.go
git commit -m "refactor: extract server core to pkg/server"
```

---

### Task 3: Create `pkg/rdp/` — RDP client launcher library

**Design note:** `pkg/rdp` only handles the final RDP client launch (mstsc/freerdp/open). Tunnel setup (SSH jump/proxy) is handled by the existing server API. The standalone `cmd/rdp-agent` is unchanged.

**Files:**
- Create: `pkg/rdp/agent.go` — shared types + cross-platform dispatch
- Create: `pkg/rdp/agent_windows.go` — Windows mstsc launch
- Create: `pkg/rdp/agent_darwin.go` — macOS RDP launch
- Create: `pkg/rdp/agent_linux.go` — Linux freerdp launch

**Interfaces:**
- Produces: `rdp.ConnectRequest struct`, `rdp.Launch(req ConnectRequest) error`
- Consumes: nothing outside stdlib

- [ ] **Step 1: Create `pkg/rdp/agent.go`**

```go
package rdp

import (
	"fmt"
	"os/exec"
	"runtime"
)

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

func Launch(req ConnectRequest) error {
	if req.Port == 0 {
		req.Port = 3389
	}
	target := fmt.Sprintf("%s:%d", req.Host, req.Port)
	switch runtime.GOOS {
	case "windows":
		return launchWindows(target, req.Username, req.Password)
	case "darwin":
		return launchDarwin(target, req.Username)
	case "linux":
		return launchLinux(target, req.Username)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func storeCredentials(host, username, password string) {
	if runtime.GOOS != "windows" {
		return
	}
	exec.Command("cmdkey", "/generic:TERMSRV/"+host, "/user:"+username, "/pass:"+password).Run()
}
```

- [ ] **Step 2: Create `pkg/rdp/agent_windows.go`**

```go
package rdp

import (
	"fmt"
	"os"
	"os/exec"
)

func launchWindows(target, username, password string) error {
	storeCredentials("127.0.0.1", username, password)
	rdpFile := os.Getenv("TEMP") + "\\rdp_connect.rdp"
	content := fmt.Sprintf(
		"full address:s:%s\r\nusername:s:%s\r\nprompt for credentials:i:0\r\nauthentication level:i:2\r\n",
		target, username)
	if err := os.WriteFile(rdpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("create .rdp file failed: %w", err)
	}
	return exec.Command("mstsc", rdpFile).Start()
}
```

- [ ] **Step 3: Create `pkg/rdp/agent_darwin.go`**

```go
package rdp

import (
	"fmt"
	"os/exec"
)

func launchDarwin(target, username string) error {
	url := fmt.Sprintf("ms-rdp:full%%20address=s:%s", target)
	if err := exec.Command("open", url).Start(); err == nil {
		return nil
	}
	return exec.Command("open", fmt.Sprintf("rdp://%s@%s", username, target)).Start()
}
```

- [ ] **Step 4: Create `pkg/rdp/agent_linux.go`**

```go
package rdp

import "os/exec"

func launchLinux(target, username string) error {
	if cmd, err := exec.LookPath("xfreerdp"); err == nil {
		return exec.Command(cmd, "/v:"+target, "/u:"+username, "/dynamic-resolution").Start()
	}
	if cmd, err := exec.LookPath("rdesktop"); err == nil {
		return exec.Command(cmd, target, "-u", username).Start()
	}
	return exec.LookPath("xfreerdp")
}
```

- [ ] **Step 5: Build to verify**

```bash
go build ./pkg/rdp/
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/rdp/
git commit -m "feat: add pkg/rdp cross-platform RDP launcher"
```

---

### Task 4: Create `cmd/desktop/` — Wails app entry point

**Files:**
- Create: `cmd/desktop/main.go` — Wails app entry
- Create: `cmd/desktop/app.go` — App struct with RDP binding + tray
- Create: `cmd/desktop/wails.json` — Wails project config

**Interfaces:**
- Consumes: `pkg/server.Start()`, `pkg/assets.*`, `rdp.Launch()`
- Produces: standalone `deploy-manager-desktop` binary

- [ ] **Step 1: Install Wails CLI**

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails --version
```

Expected: prints Wails v2.x.x

- [ ] **Step 2: Create `cmd/desktop/main.go`**

```go
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"deploy-manager/pkg/assets"
	"deploy-manager/pkg/server"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := server.Options{
		Port:              port,
		Desktop:           true,
		WebAssets:         assets.WebAssets,
		RDPAgentExe:       assets.RDPAgentExe,
		DefaultConfig:     assets.DefaultConfig,
		DefaultScenarios:  assets.DefaultScenarios,
		DefaultTemplates:  assets.DefaultTemplates,
	}

	go func() {
		if _, err := server.Start(ctx, opts); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	app := NewApp()
	serverURL := "http://127.0.0.1:" + itoa(port)

	err = wails.Run(&options.App{
		Title:     "Deploy Manager",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			URL: serverURL,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &options.Windows{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})

	if err != nil {
		log.Fatalf("Wails error: %v", err)
	}
}

func itoa(i int) string {
	var b [20]byte
	bp := len(b) - 1
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i >= 10 {
		b[bp] = byte('0' + i%10)
		i /= 10
		bp--
	}
	b[bp] = byte('0' + i)
	if neg {
		bp--
		b[bp] = '-'
	}
	return string(b[bp:])
}
```

- [ ] **Step 3: Create `cmd/desktop/app.go`**

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"deploy-manager/pkg/rdp"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	sigChan chan os.Signal
}

func NewApp() *App {
	return &App{
		sigChan: make(chan os.Signal, 1),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	signal.Notify(a.sigChan, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Desktop app started")
}

func (a *App) shutdown(ctx context.Context) {
	log.Println("Desktop app shutting down")
}

// LaunchRDP is exposed to the frontend via Wails runtime binding.
func (a *App) LaunchRDP(host string, port int, username string, password string) error {
	return rdp.Launch(rdp.ConnectRequest{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	})
}
```

- [ ] **Step 4: Create `cmd/desktop/wails.json`**

```json
{
  "name": "deploy-manager",
  "outputfilename": "deploy-manager-desktop",
  "frontend:install": "",
  "frontend:build": "",
  "frontend:dev:watcher": "",
  "frontend:dev:serverUrl": "",
  "author": {
    "name": "deploy-manager",
    "email": ""
  }
}
```

- [ ] **Step 5: Create Wails build assets directory**

```bash
New-Item -ItemType Directory -Path "cmd/desktop/build" -Force
```

Wails requires a `build/appicon.png` file. Create a minimal 512×512 PNG placeholder:
```bash
# Option 1: create with Go
go run -mod=mod github.com/wailsapp/wails/v2/cmd/wails@latest init -n temp -t vanilla
copy temp\build\appicon.png cmd\desktop\build\
Remove-Item -Recurse -Force temp
```

- [ ] **Step 6: Build and verify**

```bash
cd cmd\desktop
wails build -clean
```

Expected: produces `build/bin/deploy-manager-desktop.exe`

- [ ] **Step 7: Commit**

```bash
git add cmd/desktop/
git commit -m "feat: add Wails desktop app entry point"
```

---

### Task 5: Update build system and frontend RDP integration

**Files:**
- Modify: `Makefile` — add `desktop` target
- Modify: `cmd/server/web/templates/index.html` — add Wails RDP binding call
- Modify: `cmd/server/web/templates/terminal.html` — add Wails RDP binding call

- [ ] **Step 1: Update Makefile**

Replace the Makefile with:

```makefile
.PHONY: build run dev clean test linux windows desktop

BINARY_NAME=deploy-manager

build:
	go build -o $(BINARY_NAME) ./cmd/server/main.go

run:
	go run ./cmd/server/main.go

dev:
	go run ./cmd/server/main.go --port 3001

desktop:
	cd cmd\desktop && wails build -clean

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux ./cmd/server/main.go

windows:
	GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME).exe ./cmd/server/main.go

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux $(BINARY_NAME).exe
	rm -rf ./data

test:
	go test ./...

deps:
	go mod tidy
	go mod download
```

- [ ] **Step 2: Update frontend RDP flow in `index.html`**

Find line 846 of `cmd/server/web/templates/index.html`:
```js
                const rdpRes = await fetch('http://localhost:8765/connect', {
```

Insert before it:
```js
                if (window.runtime && window.runtime.LaunchRDP) {
                    try { await window.runtime.LaunchRDP(host, port, username, password); return; } catch(e) {}
                }
```

- [ ] **Step 3: Same change in `terminal.html`**

Find line 1168 of `cmd/server/web/templates/terminal.html` and apply the same insertion.

- [ ] **Step 4: Verify both builds work**

```bash
go build -o deploy-manager.exe ./cmd/server/main.go
cd cmd\desktop && wails build -clean
```

Both should succeed.

- [ ] **Step 5: Commit**

```bash
git add Makefile
git add cmd/server/web/templates/index.html cmd/server/web/templates/terminal.html
git commit -m "feat: add desktop build target and frontend RDP binding"
```<｜end▁of▁thinking｜>

<｜｜DSML｜｜tool_calls>
<｜｜DSML｜｜invoke name="edit">
<｜｜DSML｜｜parameter name="filePath" string="true">C:\Users\BR\Desktop\code\lmg\deploy-manager\docs\superpowers\plans\2026-07-01-desktop-client.md