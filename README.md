# UDP Tunnel

[中文](#中文) | [English](#english)

## 中文

### 项目简介

UDP Tunnel 是一个自托管远程 TCP 访问工具。服务端提供 Rendezvous / STUN / TURN、MySQL 控制面和 HTTP API；客户端作为 Windows agent 常驻运行，从控制面拉取转发规则，优先 P2P 打洞，失败后自动走服务器中继。

### 项目结构

- `cmd/server`：服务端可执行程序入口，包含 UDP rendezvous、relay 和 HTTP API
- `cmd/client`：Windows 客户端可执行程序入口，包含 agent、托盘、服务、配置页和更新逻辑
- `frontend-admin`：独立 React + Ant Design 管理后台，使用 JWT 调用服务端 API
- `internal`：仓库内复用的业务模块，包括配置、协议、加密帧、KCP 隧道、TCP 转发、MySQL 控制库和 UPnP
- `installer`：Windows 客户端安装包脚本
- `dist`：本地构建输出目录

### 部署说明

#### 最小上线流程

1. 在一台公网可达的 Linux 机器上部署服务端，开放 `7000/udp`、`7002/udp`、`7001/tcp`
2. 配置好 `.env`，至少确认 `PSK`、`ADMIN_JWT_SECRET` 和 `CONTROL_DATABASE_DSN`
3. 启动服务端并确认 `http://<server>:7001/health` 正常
4. 在每台 Windows 客户端旁放置 `client.exe` 和最小 `client.json`
5. 在 `client.json` 中填写 `server_http` 和 `psk`
6. 启动客户端 agent，确认设备出现在 React 管理后台
7. 在 React 管理后台创建转发规则
8. 在入口设备访问 `127.0.0.1:<local_port>` 验证连通性

#### 1. 构建

```bat
build-all.bat
```

构建产物：

- `dist/server`：Linux amd64 服务端
- `dist/client.exe`：Windows amd64 客户端
- `dist/frontend-admin`：独立管理后台静态文件

`build-all.bat` 会同时构建管理后台。也可以单独构建：

```bat
cd frontend-admin
npm install
npm run build
```

开发时启动管理后台：

```bat
cd frontend-admin
npm run dev
```

默认 Vite 开发服务器监听 `http://localhost:5173`，并把 `/api`、`/health` 代理到 `http://127.0.0.1:7001`。如果生产环境前端和 API 分离部署，请设置：

```text
VITE_API_BASE_URL=http://<server>:7001
```

服务端 CORS 当前直接放行全部来源，前端和 API 分离部署时无需额外配置来源白名单。

生产部署时，`dist/frontend-admin` 是纯静态站点，可以由 Nginx、对象存储或任意静态文件服务托管。前端和 API 分离部署时，服务端只暴露 API 和 UDP 服务，不再内嵌旧 Web 管理页。

最小 Nginx 示例：

```nginx
server {
  listen 80;
  server_name admin.example.com;
  root /opt/udp-tunnel/frontend-admin;
  index index.html;

  location / {
    try_files $uri /index.html;
  }
}
```

#### 2. 部署服务端

复制配置样例：

```bat
copy .env.example .env
```

编辑 `.env`：

```dotenv
UDP_LISTEN=:7000
STUN_ALT_LISTEN=:7002
HTTP_LISTEN=:7001
CONTROL_DATABASE_DSN=udp_tunnel:change-me@tcp(127.0.0.1:3306)/udp_tunnel?charset=utf8mb4&parseTime=True&loc=Local
ADMIN_JWT_SECRET=change-this-admin-jwt-secret
PSK=change-this-deployment-secret
```

启动服务端：

```bash
./server -env .env
```

服务端需要开放以下端口：

| Port | Protocol | 用途 |
|---|---|---|
| 7000 | UDP | Rendezvous / STUN / TURN |
| 7002 | UDP | NAT 探测备用 STUN |
| 7001 | TCP | Web 控制面和 API |

访问已部署的 React 管理后台，首次初始化账号为 `admin`，密码为 `admin`。登录后请立即在设置页修改管理员密码。

`.env` 只保存服务启动前必须知道的端口、数据库连接、JWT 密钥和 PSK。数据库固定为 MySQL，Gorm 会在启动时自动建表。隧道策略、客户端默认参数、客户端发布信息会存储在 MySQL `system_settings` 表，并通过管理后台设置页维护。

服务端 HTTP API 已切换到 Gin。控制库为 MySQL 5.5，`internal/controlstore` 按 MySQL 5.5 兼容方式配置 Gorm 连接和模型。API 契约见 [docs/api-contract.md](docs/api-contract.md)。

如需在开发机验证 MySQL 自动建表和读写路径，可设置 `UDP_TUNNEL_MYSQL_DSN` 后运行：

```bat
set UDP_TUNNEL_MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/udp_tunnel?charset=utf8mb4&parseTime=True&loc=Local
go test ./internal/controlstore
```

#### 3. 部署客户端

复制配置样例：

```bat
copy client.json.example client.json
```

编辑 `client.json`：

```json
{
  "server_http": "http://tunnel.example.com",
  "device_name": "",
  "psk": "change-this-deployment-secret"
}
```

说明：

- `server_http`：客户端唯一必填入口，启动后会从这里拉取运行配置
- `device_name`：可选，留空时默认使用 Windows 计算机名
- `psk`：必须与服务端一致

`client.json.example` 只保留以上三项本机引导配置。不要再把 UDP 地址、打洞超时、端口映射、中继策略、日志等级、托盘开关、客户端发布信息写入客户端配置样例；这些配置都由服务端数据库统一管理。

客户端首次启动会自动生成稳定 `device_id`。  
运行期的 UDP 地址、打洞、UPnP、relay 默认项会从 MySQL 配置读取，并通过 `POST /api/agent/bootstrap` 由服务端统一下发。
客户端启动时会自动做一次 NAT 探测；如果判定为 `Symmetric NAT`，本进程会直接优先走 relay。

启动客户端：

```bat
client.exe -config client.json -agent
```

首次启动如果本地缺少最小引导配置，会自动打开本地配置页。保存后客户端会自动重启。

#### 4. 配置转发规则

在 React 管理后台添加规则，例如：

- 入口设备：`laptop`
- 出口设备：`office-pc`
- 本地端口：`13389`
- 目标：`127.0.0.1:3389`

两端 agent 在线后，在入口设备访问：

```text
127.0.0.1:13389
```

### 使用说明

#### 托盘菜单

客户端启动后可从 Windows 托盘打开：

- `打开管理后台`：打开 Web 管理后台
- `客户端配置`：打开本机引导配置页，只维护 `server_http`、`device_name`、`psk`
- `打开日志目录`：打开客户端日志目录
- `重启服务`：重启 Windows 服务
- `检查更新`：立即检查客户端更新
- `退出托盘`：退出托盘助手

#### 调试模式

保留原始命令行直连方式：

```bat
client.exe -server 1.2.3.4:7000 -id B -peer A -psk change-this-deployment-secret
client.exe -server 1.2.3.4:7000 -id A -peer B -psk change-this-deployment-secret -forward 13389:127.0.0.1:3389
```

强制中继：

```bat
client.exe -server 1.2.3.4:7000 -id A -peer B -psk change-this-deployment-secret -force-relay -forward 13389:127.0.0.1:3389
```

NAT 探测：

```bat
client.exe -server 1.2.3.4:7000 -psk change-this-deployment-secret -probe
```

### API

管理后台认证接口：

- `POST /api/admin/auth/login`
- `POST /api/admin/auth/refresh`
- `POST /api/admin/auth/logout`
- `GET /api/admin/me`

管理接口：

- `GET /api/admin/devices`
- `GET /api/admin/devices/{id}`
- `GET /api/admin/rules`
- `POST /api/admin/rules`
- `PATCH /api/admin/rules/{id}`
- `DELETE /api/admin/rules/{id}`
- `GET /api/admin/sessions`
- `GET /api/admin/metrics`
- `GET /api/admin/settings`
- `PATCH /api/admin/settings`
- `POST /api/admin/password`

Agent 接口：

- `POST /api/agent/register`
- `POST /api/agent/heartbeat`
- `POST /api/agent/tunnel-status`
- `POST /api/agent/bootstrap`
- `GET /api/agent/rules?device_id=<id>`

- `GET /api/admin/tunnel-states`

管理后台 API 使用 JWT Bearer Token。Agent API 使用 `X-UDP-Tunnel-PSK` header。
错误响应统一为 JSON：`{"code":"bad_rule","error":"target_port must be 1-65535"}`。

### 验证

```bat
go test ./...
build-all.bat
```

### 限制

- 管理后台和 API 已分离部署，服务端不再提供内嵌静态管理页
- 设备安全模型是部署级 PSK，不是每设备独立密钥

---

## English

### Overview

UDP Tunnel is a self-hosted remote TCP access tool. The server provides Rendezvous / STUN / TURN, a MySQL-backed control plane, and HTTP APIs. The Windows client runs as a resident agent, pulls forwarding rules from the control plane, prefers P2P hole punching, and falls back to server relay when needed.

### Project Layout

- `cmd/server`: server executable entrypoint with UDP rendezvous, relay, and HTTP API
- `cmd/client`: Windows client executable entrypoint with agent, tray, service, settings page, and update logic
- `frontend-admin`: standalone React + Ant Design admin console that calls the server API with JWT authentication
- `internal`: repository-private reusable modules for config, protocol, secure frames, KCP tunneling, TCP forwarding, MySQL control storage, and UPnP
- `installer`: Windows client installer script
- `dist`: local build output directory

### Deployment

#### Quick Start Checklist

1. Deploy the server on a publicly reachable Linux host and open `7000/udp`, `7002/udp`, and `7001/tcp`
2. Configure `.env`, at minimum setting `PSK`, `ADMIN_JWT_SECRET`, and `CONTROL_DATABASE_DSN`
3. Start the server and verify `http://<server>:7001/health` is healthy
4. Place `client.exe` and a minimal `client.json` on each Windows client
5. Fill in `server_http` and `psk` in `client.json`
6. Start the client agent and confirm the device appears in the Web UI
7. Create forwarding rules in the control plane
8. Validate connectivity from the source device using `127.0.0.1:<local_port>`

#### 1. Build

```bat
build-all.bat
```

Build outputs:

- `dist/server`: Linux amd64 server
- `dist/client.exe`: Windows amd64 client
- `dist/frontend-admin`: standalone admin console static files

`build-all.bat` builds the admin console as well. You can also build it separately:

```bat
cd frontend-admin
npm install
npm run build
```

Run the admin console in development:

```bat
cd frontend-admin
npm run dev
```

The Vite dev server listens on `http://localhost:5173` and proxies `/api` and `/health` to `http://127.0.0.1:7001`. For separated production deployment, set:

```text
VITE_API_BASE_URL=http://<server>:7001
```

The server currently allows all CORS origins, so separated frontend/API deployments do not need an origin allowlist.

For production, `dist/frontend-admin` is a plain static site and can be served by Nginx, object storage, or any static file server. With separated frontend/API deployment, the Go server only serves APIs and UDP services; it no longer embeds the legacy Web UI.

Minimal Nginx example:

```nginx
server {
  listen 80;
  server_name admin.example.com;
  root /opt/udp-tunnel/frontend-admin;
  index index.html;

  location / {
    try_files $uri /index.html;
  }
}
```

#### 2. Deploy the server

Copy the sample config:

```bat
copy .env.example .env
```

Edit `.env`:

```dotenv
UDP_LISTEN=:7000
STUN_ALT_LISTEN=:7002
HTTP_LISTEN=:7001
CONTROL_DATABASE_DSN=udp_tunnel:change-me@tcp(127.0.0.1:3306)/udp_tunnel?charset=utf8mb4&parseTime=True&loc=Local
ADMIN_JWT_SECRET=change-this-admin-jwt-secret
PSK=change-this-deployment-secret
```

Start the server:

```bash
./server -env .env
```

Open these ports:

| Port | Protocol | Purpose |
|---|---|---|
| 7000 | UDP | Rendezvous / STUN / TURN |
| 7002 | UDP | Alternate STUN port for NAT probing |
| 7001 | TCP | HTTP API |

Open the deployed React admin console and log in with the initial account `admin` and password `admin`. Change the admin password from the settings page immediately after login.

`.env` only stores values required before the service can start: listen addresses, database connection, JWT secret, and PSK. The database is fixed to MySQL, and Gorm auto-migrates tables on startup. Tunnel policy, client defaults, and client release metadata are stored in the MySQL `system_settings` table and managed from the admin settings page.

The server HTTP API now runs on Gin. The control database is MySQL 5.5; `internal/controlstore` configures Gorm with MySQL 5.5 compatible options and models. See [docs/api-contract.md](docs/api-contract.md) for the API contract.

To verify MySQL auto-migration and store behavior during development, set `UDP_TUNNEL_MYSQL_DSN` and run:

```bat
set UDP_TUNNEL_MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/udp_tunnel?charset=utf8mb4&parseTime=True&loc=Local
go test ./internal/controlstore
```

#### 3. Deploy the client

Copy the sample config:

```bat
copy client.json.example client.json
```

Edit `client.json`:

```json
{
  "server_http": "http://tunnel.example.com",
  "device_name": "",
  "psk": "change-this-deployment-secret"
}
```

Notes:

- `server_http`: the only required local bootstrap entry
- `device_name`: optional; defaults to the Windows hostname
- `psk`: must match the server

`client.json.example` intentionally contains only these three local bootstrap fields. Do not add UDP addresses, punch timeouts, port mapping, relay policy, log level, tray settings, or client release metadata back into the sample client config; those values are centrally managed in the server database.

On first start, the client generates a stable `device_id`.  
Runtime settings such as UDP rendezvous address, punching, UPnP, and relay defaults are read from MySQL settings and delivered by the server through `POST /api/agent/bootstrap`.
The client also performs automatic NAT probing at startup; if it detects a symmetric NAT, it will prefer relay immediately.

Start the client:

```bat
client.exe -config client.json -agent
```

If the local bootstrap config is incomplete, the client automatically opens the local config page. After saving, the client restarts itself.

#### 4. Create forwarding rules

In the React admin console, add a rule such as:

- Source device: `laptop`
- Target device: `office-pc`
- Local port: `13389`
- Target: `127.0.0.1:3389`

Then connect from the source device to:

```text
127.0.0.1:13389
```

### Usage

#### Tray menu

Once started, the Windows tray provides:

- `打开管理后台`: opens the Web admin console
- `客户端配置`: opens the local bootstrap settings page for `server_http`, `device_name`, and `psk`
- `打开日志目录`: opens the client log directory
- `重启服务`: restarts the Windows service
- `检查更新`: checks for client updates
- `退出托盘`: exits the tray helper

#### Debug mode

Legacy direct CLI mode is still available:

```bat
client.exe -server 1.2.3.4:7000 -id B -peer A -psk change-this-deployment-secret
client.exe -server 1.2.3.4:7000 -id A -peer B -psk change-this-deployment-secret -forward 13389:127.0.0.1:3389
```

Force relay:

```bat
client.exe -server 1.2.3.4:7000 -id A -peer B -psk change-this-deployment-secret -force-relay -forward 13389:127.0.0.1:3389
```

NAT probe:

```bat
client.exe -server 1.2.3.4:7000 -psk change-this-deployment-secret -probe
```

### API

Admin auth endpoints:

- `POST /api/admin/auth/login`
- `POST /api/admin/auth/refresh`
- `POST /api/admin/auth/logout`
- `GET /api/admin/me`

Management endpoints:

- `GET /api/admin/devices`
- `GET /api/admin/devices/{id}`
- `GET /api/admin/rules`
- `POST /api/admin/rules`
- `PATCH /api/admin/rules/{id}`
- `DELETE /api/admin/rules/{id}`
- `GET /api/admin/sessions`
- `GET /api/admin/metrics`
- `GET /api/admin/settings`
- `PATCH /api/admin/settings`
- `POST /api/admin/password`

Agent endpoints:

- `POST /api/agent/register`
- `POST /api/agent/heartbeat`
- `POST /api/agent/tunnel-status`
- `POST /api/agent/bootstrap`
- `GET /api/agent/rules?device_id=<id>`

- `GET /api/admin/tunnel-states`

Admin APIs use JWT Bearer tokens. Agent APIs use the `X-UDP-Tunnel-PSK` header.
Error responses use JSON consistently: `{"code":"bad_rule","error":"target_port must be 1-65535"}`.

### Verification

```bat
go test ./...
build-all.bat
```

### Limitations

- The admin console and API are deployed separately; the server no longer serves an embedded static admin UI
- The security model uses a deployment-wide PSK, not per-device keys
