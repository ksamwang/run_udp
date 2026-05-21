# UDP Tunnel

[中文](#中文) | [English](#english)

## 中文

### 项目简介

UDP Tunnel 是一个自托管远程 TCP 访问工具。服务端提供 Rendezvous / STUN / TURN、SQLite 控制面和 Web 管理页；客户端作为 Windows agent 常驻运行，从控制面拉取转发规则，优先 P2P 打洞，失败后自动走服务器中继。

### 项目结构

- `cmd/server`：服务端可执行程序入口，包含 UDP rendezvous、relay、HTTP API 和内嵌 Web 管理页
- `cmd/client`：Windows 客户端可执行程序入口，包含 agent、托盘、服务、配置页和更新逻辑
- `internal`：仓库内复用的业务模块，包括配置、协议、加密帧、KCP 隧道、TCP 转发、SQLite 存储和 UPnP
- `installer`：Windows 客户端安装包脚本
- `dist`：本地构建输出目录

### 部署说明

#### 最小上线流程

1. 在一台公网可达的 Linux 机器上部署服务端，开放 `7000/udp`、`7002/udp`、`7001/tcp`
2. 配置好 `server.json`，至少确认 `psk` 和 `admin_password`
3. 启动服务端并确认可以访问 `http://<server>:7001/`
4. 在每台 Windows 客户端旁放置 `client.exe` 和最小 `client.json`
5. 在 `client.json` 中填写 `server_http` 和 `psk`
6. 启动客户端 agent，确认设备出现在 Web 管理页
7. 在 Web 上创建转发规则
8. 在入口设备访问 `127.0.0.1:<local_port>` 验证连通性

#### 1. 构建

```bat
build-all.bat
```

构建产物：

- `dist/server`：Linux amd64 服务端
- `dist/client.exe`：Windows amd64 客户端

#### 2. 部署服务端

复制配置样例：

```bat
copy server.json.example server.json
```

编辑 `server.json`：

```json
{
  "udp_listen": ":7000",
  "stun_alt_listen": ":7002",
  "http_listen": ":7001",
  "database_path": "udp-tunnel.db",
  "admin_password": "change-me",
  "psk": "change-this-deployment-secret",
  "peer_ttl": "90s",
  "pair_ttl": "2m",
  "relay_idle_timeout": "5m",
  "allow_relay": true,
  "allow_legacy": false,
  "client_no_upnp": false,
  "client_upnp_timeout": "4s",
  "client_log_level": "info",
  "client_tray_enabled": true,
  "client_punch_timeout": "30s",
  "client_force_relay": false,
  "client_allow_legacy": false
}
```

启动服务端：

```bash
./server -config server.json
```

服务端需要开放以下端口：

| Port | Protocol | 用途 |
|---|---|---|
| 7000 | UDP | Rendezvous / STUN / TURN |
| 7002 | UDP | NAT 探测备用 STUN |
| 7001 | TCP | Web 控制面和 API |

访问 `http://<server>:7001/`，使用 `admin_password` 登录。首次启动会把密码写入 SQLite 的 bcrypt hash；如果后续修改密码，建议同步更新配置或重建密码 hash。

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

客户端首次启动会自动生成稳定 `device_id`。  
运行期的 UDP 地址、打洞、UPnP、relay 默认项会通过 `POST /api/agent/bootstrap` 由服务端统一下发。  
客户端启动时会自动做一次 NAT 探测；如果判定为 `Symmetric NAT`，本进程会直接优先走 relay。

启动客户端：

```bat
client.exe -config client.json -agent
```

首次启动如果本地缺少最小引导配置，会自动打开本地配置页。保存后客户端会自动重启。

#### 4. 配置转发规则

在服务端 Web 管理页添加规则，例如：

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

- `Open Control Plane`：服务端 Web 管理页
- `Client Settings`：本机 `client.json` 可视化配置页
- `Exit`：退出客户端

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

Web 登录接口：

- `POST /api/login`
- `POST /api/logout`
- `GET /api/me`

管理接口：

- `GET /api/devices`
- `GET /api/devices/{id}`
- `GET /api/forwards`
- `POST /api/forwards`
- `PATCH /api/forwards/{id}`
- `DELETE /api/forwards/{id}`
- `GET /api/sessions`
- `GET /api/metrics`
- `GET /api/settings`
- `PATCH /api/settings`
- `POST /api/admin/password`

Agent 接口：

- `POST /api/agent/register`
- `POST /api/agent/heartbeat`
- `POST /api/agent/tunnel-status`
- `POST /api/agent/bootstrap`
- `GET /api/agent/rules?device_id=<id>`

隧道状态接口：

- `GET /api/tunnel-states`

Agent API 使用 `X-UDP-Tunnel-PSK` header。

### 验证

```bat
go test ./...
build-all.bat
```

### 限制

- Web 管理页是内嵌原生 HTML/CSS/JS，适合 MVP 管理，不是完整桌面控制台
- 设备安全模型是部署级 PSK，不是每设备独立密钥

---

## English

### Overview

UDP Tunnel is a self-hosted remote TCP access tool. The server provides Rendezvous / STUN / TURN, an SQLite-backed control plane, and a Web UI. The Windows client runs as a resident agent, pulls forwarding rules from the control plane, prefers P2P hole punching, and falls back to server relay when needed.

### Project Layout

- `cmd/server`: server executable entrypoint with UDP rendezvous, relay, HTTP API, and embedded Web UI
- `cmd/client`: Windows client executable entrypoint with agent, tray, service, settings page, and update logic
- `internal`: repository-private reusable modules for config, protocol, secure frames, KCP tunneling, TCP forwarding, SQLite storage, and UPnP
- `installer`: Windows client installer script
- `dist`: local build output directory

### Deployment

#### Quick Start Checklist

1. Deploy the server on a publicly reachable Linux host and open `7000/udp`, `7002/udp`, and `7001/tcp`
2. Configure `server.json`, at minimum setting `psk` and `admin_password`
3. Start the server and verify `http://<server>:7001/` is reachable
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

#### 2. Deploy the server

Copy the sample config:

```bat
copy server.json.example server.json
```

Edit `server.json`:

```json
{
  "udp_listen": ":7000",
  "stun_alt_listen": ":7002",
  "http_listen": ":7001",
  "database_path": "udp-tunnel.db",
  "admin_password": "change-me",
  "psk": "change-this-deployment-secret",
  "peer_ttl": "90s",
  "pair_ttl": "2m",
  "relay_idle_timeout": "5m",
  "allow_relay": true,
  "allow_legacy": false,
  "client_no_upnp": false,
  "client_upnp_timeout": "4s",
  "client_log_level": "info",
  "client_tray_enabled": true,
  "client_punch_timeout": "30s",
  "client_force_relay": false,
  "client_allow_legacy": false
}
```

Start the server:

```bash
./server -config server.json
```

Open these ports:

| Port | Protocol | Purpose |
|---|---|---|
| 7000 | UDP | Rendezvous / STUN / TURN |
| 7002 | UDP | Alternate STUN port for NAT probing |
| 7001 | TCP | Web control plane and API |

Open `http://<server>:7001/` and log in with `admin_password`.

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

On first start, the client generates a stable `device_id`.  
Runtime settings such as UDP rendezvous address, punching, UPnP, and relay defaults are delivered by the server through `POST /api/agent/bootstrap`.  
The client also performs automatic NAT probing at startup; if it detects a symmetric NAT, it will prefer relay immediately.

Start the client:

```bat
client.exe -config client.json -agent
```

If the local bootstrap config is incomplete, the client automatically opens the local config page. After saving, the client restarts itself.

#### 4. Create forwarding rules

In the Web control plane, add a rule such as:

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

- `Open Control Plane`
- `Client Settings`
- `Exit`

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

Web auth endpoints:

- `POST /api/login`
- `POST /api/logout`
- `GET /api/me`

Management endpoints:

- `GET /api/devices`
- `GET /api/devices/{id}`
- `GET /api/forwards`
- `POST /api/forwards`
- `PATCH /api/forwards/{id}`
- `DELETE /api/forwards/{id}`
- `GET /api/sessions`
- `GET /api/metrics`
- `GET /api/settings`
- `PATCH /api/settings`
- `POST /api/admin/password`

Agent endpoints:

- `POST /api/agent/register`
- `POST /api/agent/heartbeat`
- `POST /api/agent/tunnel-status`
- `POST /api/agent/bootstrap`
- `GET /api/agent/rules?device_id=<id>`

Tunnel state endpoint:

- `GET /api/tunnel-states`

Agent APIs use the `X-UDP-Tunnel-PSK` header.

### Verification

```bat
go test ./...
build-all.bat
```

### Limitations

- The Web UI is an embedded native HTML/CSS/JS MVP, not a full desktop-grade operations console
- The security model uses a deployment-wide PSK, not per-device keys
