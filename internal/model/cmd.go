package model

import (
	"time"

	"gorm.io/gorm"
)

type Command struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"size:100;not null" json:"name"`
	Command   string         `gorm:"type:text;not null" json:"command"`
	Category  string         `gorm:"size:50" json:"category"`
	IsDefault bool           `gorm:"default:false" json:"is_default"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

var DefaultCommands = []Command{
	{Name: "系统信息", Command: "uname -a && cat /etc/os-release", Category: "系统"},
	{Name: "CPU信息", Command: "lscpu", Category: "系统"},
	{Name: "内存使用", Command: "free -h", Category: "系统"},
	{Name: "磁盘使用", Command: "df -h", Category: "系统"},
	{Name: "进程列表", Command: "ps aux | head -20", Category: "进程"},
	{Name: "Top进程", Command: "top -bn1 | head -15", Category: "进程"},
	{Name: "网络连接", Command: "netstat -tuln", Category: "网络"},
	{Name: "网络状态", Command: "ss -tuln", Category: "网络"},
	{Name: "路由表", Command: "ip route", Category: "网络"},
	{Name: "DNS解析", Command: "cat /etc/resolv.conf", Category: "网络"},
	{Name: "端口占用", Command: "lsof -i", Category: "网络"},
	{Name: "Docker版本", Command: "docker --version", Category: "Docker"},
	{Name: "Docker容器", Command: "docker ps -a", Category: "Docker"},
	{Name: "Docker镜像", Command: "docker images", Category: "Docker"},
	{Name: "Docker日志", Command: "docker logs --tail 50 ", Category: "Docker"},
	{Name: "系统服务", Command: "systemctl list-units --type=service --state=running | head -20", Category: "服务"},
	{Name: "日志查看", Command: "tail -f /var/log/syslog", Category: "日志"},
	{Name: "最近登录", Command: "last -10", Category: "用户"},
	{Name: "用户列表", Command: "cat /etc/passwd", Category: "用户"},
	{Name: "定时任务", Command: "crontab -l", Category: "任务"},
}
