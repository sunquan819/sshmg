[English readme](./readme.en)
# Deploy Manager

初衷，目前没有称手的ssh免费工具，于是自己做了一个，基于 Go 的轻量级服务器部署管理工具，支持服务部署、Web终端和文件管理。

## 功能特性

- **多服务器管理**: 支持多台服务器 SSH 连接管理，支持跳板机
- **服务部署**: Docker Compose 服务一键部署
- **计划任务**: Crontab 任务管理（待实现）
- **Web 终端**: 基于 WebSocket 的实时终端，支持多标签页，鼠标选中自动复制，Ctrl+V 粘贴
- **文件管理**: 文件浏览、预览、上传、下载
- **基础设施**: 主要针对服务器的批量任务执行，支持脚本和安装包上传分发，命令执行，可在界面直接编辑脚本

## 基础设施模块

批量任务执行模块，用于在多台服务器上同时执行安装脚本或部署任务。

### 功能特点

- **场景管理**: 创建、编辑、删除执行场景（场景可以一般性的运维部署任务）
- **文件管理**: 服务器文件上传下载
- **在线编辑**: 支持预览和编辑服务器上的文件
- **批量执行**: 选择目标服务器，一键执行
- **SSH 重连**: 大文件传输自动处理连接断开问题




## 快速开始

### 编译

```bash
# Windows
go build -o deploy-manager.exe ./cmd/server/main.go

# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o deploy-manager ./cmd/server/main.go

# 或使用 Makefile
make build
make linux
```

### 运行

```bash
# 默认端口 3000，需要指定密码
./deploy-manager --password yourpass

# 指定端口
./deploy-manager --port 3001 --password yourpass
./deploy-manager -p 3001 -P yourpass

# 生产模式
GIN_MODE=release ./deploy-manager --password yourpass
```

### 启动参数

| 参数 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| --port | -p | 指定服务端口 | 3000 |
| --password | -P | 设置管理员密码 | 首次启动必须指定 |

### 登录信息

首次启动时需要通过命令行指定管理员密码：

```bash
./deploy-manager --password yourpassword
# 或
./deploy-manager -P yourpassword
```

默认用户名：`admin`

### 数据目录

- 数据存储在 `./data` 目录
- 日志文件为 `./data/deploy.log`

## 配置文件(一般无需关注）

```yaml
server:
  port: 3000
  data_dir: ./data

database:
  path: ./data/deploy.db

jwt:
  secret: ""  # 留空自动生成
  expire_hours: 2
  refresh_expire_days: 7

log:
  level: info
  path: ./data/deploy.log

admin:
  username: admin
  password: ""  # 留空自动生成
```

## API 接口

### 认证
- POST /api/login - 登录
- POST /api/refresh - 刷新 Token
- GET /api/user - 获取用户信息

### 服务器管理
- GET /api/servers - 服务器列表
- POST /api/servers - 添加服务器
- PUT /api/servers/:id - 更新服务器
- DELETE /api/servers/:id - 删除服务器
- GET /api/servers/:id/test - 测试连接

### 服务器管理（跳板机）
- GET /api/servers/:id/test - 测试连接（支持跳板机）

### 部署管理
- GET /api/deployments - 部署列表
- POST /api/deployments - 创建部署
- DELETE /api/deployments/:id - 删除部署
- POST /api/deployments/:id/restart - 重启部署
- GET /api/deployments/:id/logs - 查看日志

### 计划任务
- GET /api/crons - 任务列表
- POST /api/crons - 创建任务
- DELETE /api/crons/:id - 删除任务
- POST /api/crons/:id/execute - 执行任务

### 文件管理
- GET /api/files/:id - 文件列表
- POST /api/files/:id/upload - 上传文件
- GET /api/files/:id/download - 下载文件
- DELETE /api/files/:id - 删除文件

### Web 终端
- GET /api/terminal/:id - WebSocket 连接

### 基础设施（批量任务）
- GET /api/infrastructure/scenarios - 场景列表
- POST /api/infrastructure/scenarios - 创建场景
- PUT /api/infrastructure/scenarios/:id - 更新场景
- DELETE /api/infrastructure/scenarios/:id - 删除场景
- POST /api/infrastructure/scenarios/:id/files - 上传文件（脚本/安装包）
- GET /api/infrastructure/scenarios/:id/files/:filename - 获取文件内容
- PUT /api/infrastructure/scenarios/:id/files/:filename - 更新文件内容
- DELETE /api/infrastructure/scenarios/:id/files/:filename - 删除文件
- POST /api/infrastructure/execute - 执行场景
- GET /api/infrastructure/executions - 执行记录列表
- GET /api/infrastructure/executions/:id - 执行详情

## 技术栈

- **后端**: Go 1.21+
- **Web 框架**: Gin
- **数据库**: SQLite + GORM
- **SSH**: golang.org/x/crypto/ssh
- **认证**: JWT
- **WebSocket**: gorilla/websocket
- **前端**: HTMX + Alpine.js + TailwindCSS
- **终端**: xterm.js

## 项目结构

```
deploy-manager/
├── cmd/server/main.go      # 程序入口
├── internal/
│   ├── config/             # 配置管理
│   ├── database/           # 数据库
│   ├── handler/            # HTTP 处理器
│   │   ├── infrastructure.go  # 基础设施模块
│   │   ├── terminal.go     # 终端模块
│   │   ├── file.go         # 文件管理
│   │   └── ...
│   ├── service/            # 业务逻辑
│   │   └── ssh.go          # SSH 服务
│   └── model/              # 数据模型
├── pkg/ssh/                # SSH 客户端
├── web/
│   ├── templates/          # HTML 模板
│   │   ├── infrastructure.html  # 基础设施页面
│   │   └── terminal.html    # 终端页面
│   └── static/             # 静态文件
├── uploads/                # 上传文件目录
├── config.yaml             # 配置文件
├── go.mod
└── Makefile
```

## 安全说明

1. 首次启动设置复杂管理员密码
2. 建议在生产环境使用代理做 HTTPS


## License

MIT License
