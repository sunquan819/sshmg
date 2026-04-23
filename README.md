[English readme](./readme.en)
# 部署管理系统

一款基于 Web 的服务器运维管理工具，支持服务器管理、容器部署、计划任务、Web 终端、文件管理、笔记管理等企业级运维场景。

## 功能特性

### 🖥️ 服务器管理
- 多台服务器 SSH 连接管理
- 支持跳板机/堡垒机
- 服务器分组、在线状态监控
- 一键连接终端、测试连接

### 📦 容器管理
- Docker 容器查看、启动、停止、删除
- 容器日志查看
- 镜像列表管理

### 🚀 服务部署
- Docker Compose 服务一键部署
- 支持自定义 compose 配置
- 部署状态跟踪

### ⏰ 计划任务
- Crontab 任务可视化编辑
- 任务执行记录查看

### 💻 Web 终端
- 基于 WebSocket 的实时终端
- 多标签页管理
- 鼠标选中自动复制，Ctrl+V 粘贴
- 内置常用命令快捷执行
- 笔记快捷查看

### 📁 文件管理
- 目录浏览、文件预览
- 文件上传、下载
- 文件在线编辑
- 支持大文件传输

### 🔧 基础设施（批量任务）
- 批量脚本执行
- 多服务器同时部署
- 脚本在线编辑
- 执行结果统计

### 📝 笔记管理
- 笔记分类、搜索
- Markdown 格式支持
- 终端页面快捷查看

### ⌨️ 常用命令
- 预置 20+ 常用 Linux 命令
- 自定义命令添加、编辑、删除
- 命令分类管理
- 点击自动复制/执行到终端

### 🔧 常用工具
- Ping / Traceroute 网络诊断
- Telnet 端口检测
- Curl HTTP 测试
- Tcpdump 抓包分析

### 🔗 端口隧道
- SOCKS5 代理隧道
- 支持绑定 127.0.0.1 或 0.0.0.0
- 映射本地端口到远程服务器

### 🪟 RDP 连接
- Windows 服务器 RDP 远程桌面
- 一键启动 RDP Agent

## 使用场景

### 场景一：日常服务器运维
1. 通过首页服务器卡片快速连接终端
2. 在 Web 终端执行日常命令操作
3. 通过文件管理上传/下载配置文件

### 场景二：批量部署应用
1. 在基础设施模块创建部署场景
2. 上传部署脚本和安装包
3. 勾选目标服务器，一键批量执行

### 场景三：多环境配置管理
1. 在笔记模块记录各环境配置
2. 通过终端页快捷查看笔记内容
3. 常用命令一键执行

### 场景四：内网穿透
1. 通过端口隧道建立 SOCKS5 代理
2. 本地应用通过代理访问内网服务器

### 场景五：容器编排
1. 部署 Docker Compose 服务
2. 监控容器运行状态
3. 查看容器日志排查问题

## 快速开始

### 编译

```bash
# Windows
go build -o deploy-manager.exe ./cmd/server/main.go

# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o deploy-manager ./cmd/server/main.go
```

### 运行

```bash
# 默认端口 3000
./deploy-manager.exe --password yourpass

# 指定端口
./deploy-manager.exe --port 3001 --password yourpass

# 生产模式
GIN_MODE=release ./deploy-manager.exe --password yourpass
```

### 登录

- 默认用户名：`admin`
- 密码：首次启动时通过 `--password` 指定

### 访问

- 浏览器打开：`http://localhost:3000`
- 终端页面：点击服务器卡片自动跳转

## 技术栈

- **后端**: Go + Gin
- **数据库**: SQLite + GORM
- **SSH**: golang.org/x/crypto/ssh
- **终端**: xterm.js + WebSocket
- **前端**: Alpine.js + TailwindCSS

## 目录结构

```
deploy-manager/
├── cmd/server/main.go      # 程序入口
├── internal/
│   ├── config/             # 配置管理
│   ├── database/           # 数据库初始化
│   ├── handler/            # HTTP 处理器
│   ├── service/            # 业务逻辑
│   │   ├── tunnel.go       # SOCKS5 隧道
│   │   └── ssh.go          # SSH 服务
│   └── model/              # 数据模型
├── cmd/server/web/
│   ├── templates/          # HTML 模板
│   └── static/             # 静态资源
└── artifacts/              # 编译产物
```

## 安全说明

1. 首次启动后请立即修改管理员密码
2. 建议在生产环境使用 HTTPS
3. 服务器 SSH 凭证加密存储

## License

MIT
