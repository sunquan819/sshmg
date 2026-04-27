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
