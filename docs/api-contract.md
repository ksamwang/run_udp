# API Contract

当前服务端 HTTP 层使用 Gin，管理后台和 API 分离部署，CORS 直接放行全部来源。UDP rendezvous、relay、agent 拉取规则等核心隧道流程不属于本文档的重构范围。

## Conventions

- 管理后台接口前缀：`/api/admin`
- Agent 接口前缀：`/api/agent`
- 管理后台认证：`Authorization: Bearer <access_token>`
- Agent 认证：`X-UDP-Tunnel-PSK: <psk>`
- 请求体和响应体均为 JSON，文件下载接口除外
- 错误响应优先返回：

```json
{
  "code": "bad_rule",
  "error": "target_port must be 1-65535"
}
```

## Public

### `GET /health`

返回服务端健康状态、运行时间和基础指标。

### `GET /peers`

返回当前设备列表。该接口目前保留为诊断入口，不作为管理后台主数据源。

## Admin Auth

### `POST /api/admin/auth/login`

请求：

```json
{
  "password": "change-me"
}
```

响应：

```json
{
  "access_token": "...",
  "access_expires_at": "2026-05-22T12:00:00+08:00",
  "refresh_token": "...",
  "refresh_expires_at": "2026-06-21T12:00:00+08:00",
  "user": {
    "id": "admin",
    "name": "admin",
    "role": "admin"
  }
}
```

### `POST /api/admin/auth/refresh`

请求：

```json
{
  "refresh_token": "..."
}
```

响应同登录接口。刷新成功后旧 refresh token 会被撤销。

### `POST /api/admin/auth/logout`

请求：

```json
{
  "refresh_token": "..."
}
```

响应：

```json
{
  "ok": true
}
```

### `GET /api/admin/me`

返回当前管理员信息。

## Admin Management

### `GET /api/admin/devices`

返回设备列表。

### `GET /api/admin/devices/{id}`

返回单个设备详情。

### `PATCH /api/admin/devices/{id}`

请求：

```json
{
  "enabled": true
}
```

### `DELETE /api/admin/devices/{id}`

删除设备。仍被启用规则引用的设备会返回 `device_in_use`。

### `GET /api/admin/rules`

返回转发规则列表，包含运行态字段。

### `POST /api/admin/rules`

请求：

```json
{
  "name": "rdp",
  "source_id": "laptop",
  "target_id": "office-pc",
  "profile": "interactive",
  "local_port": 13389,
  "target_host": "127.0.0.1",
  "target_port": 3389,
  "enabled": true
}
```

### `PATCH /api/admin/rules/{id}`

请求体同创建规则。

### `DELETE /api/admin/rules/{id}`

删除转发规则。

### `GET /api/admin/sessions`

返回最近 200 条会话。

### `GET /api/admin/tunnel-states`

返回最近 200 条隧道状态。

### `GET /api/admin/metrics`

返回设备、规则、活跃会话、累计中继流量等指标。

### `GET /api/admin/settings`

返回服务端当前可编辑配置和只读启动配置。运行期隧道策略、客户端默认配置、客户端发布信息来自 MySQL `system_settings` 表。

### `PATCH /api/admin/settings`

更新可运行时生效的数据库配置项，并立即写入 MySQL `system_settings` 表。监听地址、数据库连接、PSK、JWT 等仍属于 `.env` 启动配置。

### `POST /api/admin/password`

请求：

```json
{
  "current_password": "old-password",
  "new_password": "new-password"
}
```

## Agent

### `POST /api/agent/register`

注册或更新设备信息。

### `POST /api/agent/heartbeat`

刷新设备在线状态。

### `POST /api/agent/tunnel-status`

上报隧道状态。

### `POST /api/agent/bootstrap`

客户端启动时拉取服务端下发配置。

### `GET /api/agent/rules?device_id=<id>`

返回与设备相关且启用的转发规则。

## Client Release

### `GET /api/client/release`

返回客户端发布信息，需要 Agent PSK。

### `GET /downloads/client/installer`

下载服务端配置的客户端安装包文件。

## Storage Direction

控制库为 MySQL 5.5，`internal/controlstore` 提供 Gorm + MySQL 5.5 兼容模型和完整读写实现，配置项如下：

```dotenv
CONTROL_DATABASE_DRIVER=mysql
CONTROL_DATABASE_DSN=user:pass@tcp(127.0.0.1:3306)/udp_tunnel?charset=utf8mb4&parseTime=True&loc=Local
CONTROL_DATABASE_AUTO_MIGRATE=true
```

运行期隧道策略、客户端默认配置、客户端发布信息存储在 `system_settings` 表，服务启动时会按代码默认值补齐缺失键，管理后台设置页通过 `PATCH /api/admin/settings` 持久化更新。

后续替换存储实现时，应以本文档接口行为和现有 store 测试为回归基线。
