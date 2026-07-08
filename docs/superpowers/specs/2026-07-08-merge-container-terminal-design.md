# 容器终端合并到 Web 终端

## 背景

`container-terminal.html` 是独立页面，在 Wails desktop 模式下点击容器管理的"终端"按钮会 `target="_blank"` 打开新窗口，Wails 不支持新开窗口导致报错。需要删除独立页面，将容器终端功能合并到 `terminal.html`。

## 目标

- 删除 `container-terminal.html` 独立页面
- 在 `terminal.html` 中支持 SSH 终端和容器终端两种 tab 类型
- 容器管理页面（containers.html）的"终端"按钮直接在 web 终端页面打开容器终端
- 后端 WebSocket 路由和 handler 保持不变

## 技术方案

### 1. terminal.html 数据层改动

**Tab 对象扩展：**
```js
// 现有 SSH tab
{ id, type: 'ssh', server, terminal, ws, fitAddon, currentPath }

// 新增 container tab
{ id, type: 'container', server, container, terminal, ws, fitAddon, currentPath }
```

**新增状态：**
- `containers: []` — 当前服务器的容器列表
- `showContainers: false` — 容器列表折叠状态
- `currentContainer: null` — 当前选中的容器

**新增方法：**
- `loadContainers(server)` — 调用 `/api/containers?server_id=xxx&all=false` 获取容器列表
- `connectContainer(container)` — 创建容器 tab 并连接
- `connectContainerTerminalForTab(tabId)` — 连接容器 WebSocket

**修改方法：**
- `createTab(server, container)` — 增加 container 参数，有 container 时 type='container'
- `connectTerminalForTab(tabId)` — 根据 tab.type 选择 WS endpoint：
  - SSH: `/ws/terminal/:serverId`
  - Container: `/ws/container-terminal/:serverId/:containerId`
- `loadFiles()` — 根据 tab 类型调用不同 API：
  - SSH: `/api/files/...`（SFTP）
  - Container: `/api/container-files/...`
- `switchTab()` — 容器 tab 切换时加载容器文件列表
- `reconnect()` — 根据 tab 类型调用对应重连逻辑

### 2. terminal.html UI 改动

**左侧栏 — 容器列表：**
- 服务器列表下方增加"容器列表"区域
- 点击服务器后，自动加载并显示该服务器的运行中容器
- 容器列表默认折叠，点击服务器后展开
- 点击容器名打开容器终端 tab

**右侧栏 — 文件管理自动切换：**
- 不需要额外按钮，文件管理面板根据当前活跃 tab 类型自动切换
- SSH tab 激活时：显示服务器文件（SFTP API `/api/files/...`）
- Container tab 激活时：显示容器文件（container-files API `/api/container-files/...`）
- 复用现有文件管理 UI（路径导航、上传、下载、新建、删除、编辑、预览）

**Tab 标签显示：**
- SSH tab：显示 `服务器名`
- Container tab：显示 `🐳 容器名`

**Header 按钮：**
- 无额外按钮，左侧栏容器列表通过点击服务器自动展开

**连接状态提示：**
- Container tab 连接时显示：`正在连接容器...`
- 连接成功显示：`容器：xxx`、`镜像：xxx`

### 3. URL 参数

`/terminal` 页面支持新参数：
- `containerId` — 可选，有此参数时自动创建容器 tab 并连接

示例：`/terminal?token=xxx&serverId=1&containerId=abc123`

### 4. containers.html 链接更新

```html
<!-- 修改前 -->
<a :href="'/container-terminal?token=' + getToken() + '&serverId=' + selectedServerId + '&containerId=' + c.id" target="_blank">终端</a>

<!-- 修改后 -->
<a :href="'/terminal?token=' + getToken() + '&serverId=' + selectedServerId + '&containerId=' + c.id">终端</a>
```

### 5. 删除 container-terminal.html

- 删除文件：`pkg/assets/web/templates/container-terminal.html`

### 6. server.go 路由清理

- 删除：`r.GET("/container-terminal", ...)` 页面路由
- 保留：`/ws/container-terminal/:id/:containerId` WebSocket 路由（terminal.html 会用到）
- 保留：`/api/container-files/...` API 路由（容器文件管理会用到）

### 7. index.html 更新

`isWebTerminalUrl()` 函数已包含 `/container-terminal` 检测，删除该条件后无需修改（因为 URL 变为 `/terminal?...&containerId=...`，已被 `/terminal?` 覆盖）。

## 不需要改动的部分

- `internal/handler/terminal.go` — ConnectContainerTerminal 等 handler 保持不变
- `internal/handler/container.go` — 容器管理 API 保持不变
- `internal/service/docker.go` — Docker 服务层保持不变
- xterm.js 和 addons — 保持不变

## 文件变更清单

| 文件 | 操作 |
|------|------|
| `pkg/assets/web/templates/terminal.html` | 修改 — 增加容器 tab 类型、容器列表、容器文件管理 |
| `pkg/assets/web/templates/containers.html` | 修改 — 更新终端链接 |
| `pkg/assets/web/templates/container-terminal.html` | 删除 |
| `pkg/server/server.go` | 修改 — 删除 /container-terminal 页面路由 |
