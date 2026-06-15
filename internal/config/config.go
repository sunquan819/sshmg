package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Log      LogConfig      `yaml:"log"`
	Admin    AdminConfig    `yaml:"admin"`
	SSH      SSHConfig      `yaml:"ssh"`
	Deploy   DeployConfig   `yaml:"deploy"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	DataDir string `yaml:"data_dir"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type JWTConfig struct {
	Secret            string `yaml:"secret"`
	ExpireHours       int    `yaml:"expire_hours"`
	RefreshExpireDays int    `yaml:"refresh_expire_days"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	Path  string `yaml:"path"`
}

type AdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SSHConfig struct {
	// SSH 空闲连接超时(分钟),默认 60(1 小时)
	IdleTTL int `yaml:"idle_ttl"`
}

type DeployConfig struct {
	// 部署日志保留最近几次(默认 5)
	LogKeepLast int `yaml:"log_keep_last"`
	// 部署日志字节上限(默认 1MB)
	LogMaxBytes int `yaml:"log_max_bytes"`
	// 部署日志块分隔符(默认 "====== 开始更新部署 ======")
	LogSeparator string `yaml:"log_separator"`
}

// EffectiveIdleTTL 返回 SSH 空闲超时(分钟),带默认值 60
func (s SSHConfig) EffectiveIdleTTL() int {
	if s.IdleTTL <= 0 {
		return 60
	}
	return s.IdleTTL
}

// DeployLogKeepLast 返回保留最近几次,带默认值
func (d DeployConfig) EffectiveLogKeepLast() int {
	if d.LogKeepLast <= 0 {
		return 5
	}
	return d.LogKeepLast
}

// DeployLogMaxBytes 返回字节上限,带默认值
func (d DeployConfig) EffectiveLogMaxBytes() int {
	if d.LogMaxBytes <= 0 {
		return 1 * 1024 * 1024
	}
	return d.LogMaxBytes
}

// DeployLogSeparator 返回分隔符,带默认值
func (d DeployConfig) EffectiveLogSeparator() string {
	if d.LogSeparator == "" {
		return "====== 开始更新部署 ======"
	}
	return d.LogSeparator
}

var GlobalConfig *Config

func getAppDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exePath)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.JWT.Secret == "" {
		cfg.JWT.Secret = generateSecret()
	}

	dataDir := GetDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	GlobalConfig = &cfg
	return &cfg, nil
}

func generateSecret() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func GetDataDir() string {
	appDir := getAppDir()
	if GlobalConfig == nil {
		return filepath.Join(appDir, "data")
	}
	path := GlobalConfig.Server.DataDir
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(appDir, path)
}

func GetDBPath() string {
	appDir := getAppDir()
	if GlobalConfig == nil {
		return filepath.Join(appDir, "data", "deploy.db")
	}
	path := GlobalConfig.Database.Path
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(appDir, GlobalConfig.Server.DataDir, filepath.Base(path))
}
