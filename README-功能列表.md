# 部署管理系统

Web 运维管理工具，支持服务器管理、容器部署、终端等。

## 功能列表

| 模块 | 功能 |
|------|------|
| 🖥️ 服务器管理 | SSH 连接、跳板机支持、分组管理、在线状态 |
| 🚀 服务部署 | Docker Compose 一键部署 |
| ⏰ 计划任务 | Crontab 可视化编辑管理 |
| 💻 Web 终端 | WebSocket 实时终端、多标签页、命令快捷执行 |
| 📁 文件管理 | 目录浏览、预览、上传、下载、在线编辑 |
| 🔧 基础设施 | 批量脚本执行、多服务器同时部署 |
| 📝 笔记管理 | 分类、搜索、Markdown |
| ⌨️ 常用命令 | 预置 20+ Linux 命令、自定义添加编辑删除 |
| 🔧 常用工具 | Ping、Traceroute、Telnet、Curl、Tcpdump |
| 🔗 端口隧道 | SOCKS5 代理、内网穿透 |
| 🪟 RDP 连接 | Windows 远程桌面 |

## 快速开始

```bash
# 启动服务
./deploy-manager.exe --password 密码

# 访问
http://localhost:3000
```

默认账号：`admin`

## 下载

- Windows: `deploy-manager.exe`
- Linux x86_64: `deploy-manager-linux-x86_64`
- Linux ARM64: `deploy-manager-linux-arm64`
