# UDP Tunnel

自托管远程 TCP 访问工具。服务端同时提供 Rendezvous/STUN/TURN、SQLite 控制面和 Web 管理页；客户端作为 Windows agent 常驻运行，从控制面拉取转发规则，优先 P2P 打洞，失败后自动走服务器中继。

## 当前能力

- UDP 打洞和 TURN 中继兜底
- KCP 可靠传输和 smux 多路复用
- Web 管理页集中管理设备、转发规则、会话和指标
- SQLite 单文件持久化，纯 Go driver，支持 `CGO_ENABLED=0`
- PSK 加密 UDP frame，控制包和 KCP 包不再靠首字节猜类型
- KCP conv ID 由 `psk + 设备ID` 稳定派生
- 客户端 agent 注册、心跳、拉取规则、自动重连
- Windows 托盘菜单：显示设备、打开控制面、退出
- 旧 demo 命令行模式保留，可用 `-allow-legacy` 调试明文 JSON 协议

## 构建

```bat
build-all.bat
```

产物：

- `dist/server`：Linux amd64 服务端
- `dist/client.exe`：Windows amd64 客户端

## 服务端

复制配置样例：

```bat
copy server.json.example server.json
```

编辑：

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
  "allow_legacy": false
}
```

启动：

```bash
./server -config server.json
```

开放端口：

| 端口 | 协议 | 用途 |
|---|---|---|
| 7000 | UDP | Rendezvous / STUN / TURN |
| 7002 | UDP | NAT 探测备用 STUN |
| 7001 | TCP | Web 控制面和 API |

访问 `http://<server>:7001/`，用 `admin_password` 登录。首次启动会把密码写入 SQLite 的 bcrypt hash；修改密码后建议删除数据库或传入新的 `admin_password_hash`。

## 客户端 Agent

复制配置样例：

```bat
copy client.json.example client.json
```

编辑：

```json
{
  "server": "1.2.3.4:7000",
  "server_http": "http://1.2.3.4:7001",
  "device_id": "",
  "psk": "change-this-deployment-secret",
  "no_upnp": false,
  "upnp_timeout": "4s",
  "log_level": "info",
  "tray_enabled": true,
  "punch_timeout": "30s",
  "force_relay": false,
  "allow_legacy": false
}
```

`device_id` 留空时，客户端会自动使用 Windows 计算机名。

启动：

```bat
client.exe -config client.json -agent
```

启动后可从 Windows 托盘打开：

- `Open Control Plane`：服务端 Web 管理页
- `Client Settings`：本机 `client.json` 可视化配置页，保存后重启客户端生效

在 Web 管理页中添加转发规则，例如：

- 入口设备：`laptop`
- 出口设备：`office-pc`
- 本地端口：`13389`
- 目标：`127.0.0.1:3389`

两端 agent 在线后，在入口设备访问 `127.0.0.1:13389`。

## 调试模式

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

## API

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
- `GET /api/agent/rules?device_id=<id>`

Agent API 使用 `X-UDP-Tunnel-PSK` header。

## 验证

```bat
go test ./...
build-all.bat
```

## 限制

- 第一版 agent 同一时间只自动连接一个 peer；多 peer 并发规则需要后续扩展。
- Web 管理页是内嵌原生 HTML/CSS/JS，适合 MVP 管理，不是完整桌面控制台。
- 设备安全模型是部署级 PSK，不是每设备独立密钥。
