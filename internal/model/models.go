package model

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"uniqueIndex;size:50;not null" json:"username"`
	Password  string         `gorm:"size:255;not null" json:"-"`
	Role      string         `gorm:"size:20;default:'admin'" json:"role"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type Server struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"size:100;not null" json:"name"`
	IP           string         `gorm:"size:50;not null" json:"ip"`
	Port         int            `gorm:"default:22" json:"port"`
	Username     string         `gorm:"size:50;not null" json:"username"`
	Password     string         `gorm:"size:512" json:"-"`
	SSHKey       string         `gorm:"type:text" json:"-"`
	Group        string         `gorm:"size:50" json:"group"`
	OsType       string         `gorm:"size:20;default:'linux'" json:"os_type"`
	ServerType   string         `gorm:"size:20;default:'server'" json:"server_type"`
	Description  string         `gorm:"size:255" json:"description"`
	Status       string         `gorm:"size:20;default:'offline'" json:"status"`
	JumpEnabled  bool           `gorm:"default:false" json:"jump_enabled"`
	JumpServerID uint           `gorm:"default:0" json:"jump_server_id"`
	JumpIP       string         `gorm:"size:50" json:"jump_ip"`
	JumpPort     int            `gorm:"default:22" json:"jump_port"`
	JumpUser     string         `gorm:"size:50" json:"jump_user"`
	JumpPassword string         `gorm:"size:512" json:"-"`
	JumpKey      string         `gorm:"type:text" json:"-"`
	ProxyEnabled bool           `gorm:"default:false" json:"proxy_enabled"`
	ProxyType    string         `gorm:"size:20" json:"proxy_type"`
	ProxyHost    string         `gorm:"size:50" json:"proxy_host"`
	ProxyPort    int            `gorm:"default:7890" json:"proxy_port"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	CronJobs     []CronJob      `gorm:"-" json:"-"`
	Deployments  []Deployment   `gorm:"-" json:"-"`
}

type Deployment struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ServerID  uint           `gorm:"not null;index" json:"server_id"`
	Type      string         `gorm:"size:20;not null" json:"type"`
	Name      string         `gorm:"size:100;not null" json:"name"`
	Config    string         `gorm:"type:text" json:"config"`
	Status    string         `gorm:"size:20;default:'pending'" json:"status"`
	Message   string         `gorm:"type:text" json:"message"`
	Logs      string         `gorm:"type:text" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Server    *Server        `gorm:"foreignKey:ServerID" json:"server,omitempty"`
}

type CronJob struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ServerID  uint           `gorm:"not null;index" json:"server_id"`
	Schedule  string         `gorm:"size:50;not null" json:"schedule"`
	Command   string         `gorm:"type:text;not null" json:"command"`
	Name      string         `gorm:"size:100" json:"name"`
	Status    string         `gorm:"size:20;default:'active'" json:"status"`
	LastRun   *time.Time     `json:"last_run,omitempty"`
	NextRun   *time.Time     `json:"next_run,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type CronHistory struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CronJobID  uint      `gorm:"not null;index" json:"cron_job_id"`
	ServerID   uint      `gorm:"not null" json:"server_id"`
	Command    string    `gorm:"type:text" json:"command"`
	Output     string    `gorm:"type:text" json:"output"`
	Error      string    `gorm:"type:text" json:"error"`
	ExitCode   int       `gorm:"default:0" json:"exit_code"`
	Duration   int64     `json:"duration"`
	ExecutedAt time.Time `gorm:"index" json:"executed_at"`
}

type FileOperation struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	ServerID    uint       `gorm:"not null;index" json:"server_id"`
	Type        string     `gorm:"size:20;not null" json:"type"`
	Path        string     `gorm:"size:512;not null" json:"path"`
	Size        int64      `json:"size"`
	Status      string     `gorm:"size:20" json:"status"`
	Message     string     `gorm:"type:text" json:"message"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type InfrastructureScenario struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"size:100;not null" json:"name"`
	Description  string         `gorm:"size:500" json:"description"`
	Playbook     string         `gorm:"type:text" json:"playbook"`
	ScriptFiles  string         `gorm:"type:text" json:"script_files"`
	PackageFiles string         `gorm:"type:text" json:"package_files"`
	ServerIDs    string         `gorm:"type:text" json:"server_ids"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

type InfrastructureExecution struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	ScenarioID  uint       `gorm:"not null;index" json:"scenario_id"`
	ServerIDs   string     `gorm:"size:500" json:"server_ids"`
	Status      string     `gorm:"size:20;default:'pending'" json:"status"`
	Output      string     `gorm:"type:text" json:"output"`
	Error       string     `gorm:"type:text" json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ComposeTemplate struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:100;not null" json:"name"`
	Description string         `gorm:"size:500" json:"description"`
	Content     string         `gorm:"type:text" json:"content"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type Database struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:100;not null" json:"name"`
	Type           string         `gorm:"size:20;not null" json:"type"` // mysql, postgresql, mongodb, redis, sqlserver, oracle, kafka
	Host           string         `gorm:"size:100;not null" json:"host"`
	Port           int            `gorm:"not null" json:"port"`
	Database       string         `gorm:"size:100" json:"database"`
	Username       string         `gorm:"size:100" json:"username"`
	Password       string         `gorm:"size:512" json:"password"`
	SSLMode        string         `gorm:"size:20" json:"ssl_mode"`
	Description    string         `gorm:"size:500" json:"description"`
	Group          string         `gorm:"size:50" json:"group"`
	ShowAllSchemas bool           `gorm:"default:false" json:"show_all_schemas"`
	Status         string         `gorm:"size:20;default:'unknown'" json:"status"`
	JumpEnabled    bool           `gorm:"default:false" json:"jump_enabled"`
	JumpServerID   uint           `gorm:"default:0" json:"jump_server_id"`
	JumpHost       string         `gorm:"size:50" json:"jump_host"`
	JumpPort       int            `gorm:"default:22" json:"jump_port"`
	JumpUser       string         `gorm:"size:50" json:"jump_user"`
	JumpPassword   string         `gorm:"size:512" json:"-"`
	JumpKey        string         `gorm:"type:text" json:"-"`
	ProxyEnabled   bool           `gorm:"default:false" json:"proxy_enabled"`
	ProxyServerID  uint           `gorm:"default:0" json:"proxy_server_id"`
	ProxyType      string         `gorm:"size:20" json:"proxy_type"`
	ProxyHost      string         `gorm:"size:50" json:"proxy_host"`
	ProxyPort      int            `gorm:"default:7890" json:"proxy_port"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

type DatabaseQuery struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	DatabaseID uint      `gorm:"not null;index" json:"database_id"`
	Query      string    `gorm:"type:text;not null" json:"query"`
	Duration   int64     `json:"duration"`
	Rows       int       `json:"rows"`
	Error      string    `gorm:"type:text" json:"error"`
	ExecutedAt time.Time `json:"executed_at"`
}

type Project struct {
	ID          uint               `gorm:"primaryKey" json:"id"`
	Name        string             `gorm:"size:100;not null" json:"name"`
	Description string             `gorm:"size:500" json:"description"`
	IsDefault   bool               `gorm:"default:false" json:"is_default"`
	Status      string             `gorm:"size:20;default:'creating'" json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	DeletedAt   gorm.DeletedAt     `gorm:"index" json:"-"`
	Components  []ProjectComponent `gorm:"foreignKey:ProjectID" json:"components"`
}

type ProjectComponent struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	ProjectID       uint           `gorm:"not null;index" json:"project_id"`
	Name            string         `gorm:"size:100;not null" json:"name"`
	Type            string         `gorm:"size:50;not null" json:"type"`
	Description     string         `gorm:"size:500" json:"description"`
	Version         string         `gorm:"size:50" json:"version"`
	InstallScript   string         `gorm:"type:text" json:"install_script"`
	InstallPkg      string         `gorm:"size:255" json:"install_pkg"`
	DeployDir       string         `gorm:"size:500" json:"deploy_dir"`
	StatusCmd       string         `gorm:"size:500" json:"status_cmd"`
	LogCmd          string         `gorm:"size:500" json:"log_cmd"`
	VersionCmd      string         `gorm:"size:500" json:"version_cmd"`
	AccessUser      string         `gorm:"size:100" json:"access_user"`
	AccessPassword  string         `gorm:"size:100" json:"access_password"`
	AccessURL       string         `gorm:"size:500" json:"access_url"`
	InstallCmd      string         `gorm:"size:500" json:"install_cmd"`
	StartCmd        string         `gorm:"size:500" json:"start_cmd"`
	StopCmd         string         `gorm:"size:500" json:"stop_cmd"`
	ConfigFile      string         `gorm:"size:500" json:"config_file"`
	Status          string         `gorm:"size:20;default:'not_deployed'" json:"status"`
	ServerIDs       string         `gorm:"type:text" json:"server_ids"`
	DeployedServers string         `gorm:"type:text" json:"deployed_servers"`
	VersionsPerServer string        `gorm:"type:text" json:"versions_per_server"` // JSON: {"server_id": "version", ...}
	DeployLog       string         `gorm:"type:text" json:"deploy_log"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

type TerminalSessionLog struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	ServerID    uint           `gorm:"index" json:"server_id"`
	ServerName  string         `gorm:"size:100" json:"server_name"`
	ServerIP    string         `gorm:"size:50" json:"server_ip"`
	SystemUser  string         `gorm:"size:50" json:"system_user"`
	SessionType string         `gorm:"size:20;default:'ssh'" json:"session_type"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	Commands    string         `gorm:"type:text" json:"commands"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type TerminalCommand struct {
	Timestamp string `json:"timestamp"`
	Command   string `json:"command"`
	Output    string `json:"output"`
}
