# UDP Tunnel Demo

一个基于 UDP 打洞（NAT Traversal）的 P2P 隧道 Demo，支持**TCP 端口转发**（如远程桌面 RDP、SSH 等），打洞失败时自动降级为**TURN 中继**兜底，保证 100% 连通。

## 特性

- ✅ UDP 打洞（NAT Hole Punching）
- ✅ NAT 类型探测（`-probe`）
- ✅ 动态学习对端真实地址（应对 Address-Dependent Mapping 类 NAT）
- ✅ KCP 可靠传输层（基于 `xtaci/kcp-go`）
- ✅ smux 流多路复用（一条隧道跑多个 TCP 连接）
- ✅ TCP 端口转发（支持同时多个 `-forward` 规则）
- ✅ TURN 中继兜底（打洞失败自动走服务器转发）
- ✅ HTTP 健康检查接口

## 架构

```
                ┌───────────────────────────┐
                │  Rendezvous + TURN Server │   (公网 VPS)
                │  UDP 7000 / HTTP 7001     │
                └───────────────────────────┘
                  ▲                       ▲
           注册/打洞/中继         注册/打洞/中继
                  │                       │
     ┌────────────┴───┐          ┌────────┴────────┐
     │   Client A     │          │   Client B      │
     │   (NAT 后)     │◄──P2P──►│   (NAT 后)      │
     └────────────────┘          └─────────────────┘
            │                             │
     [mstsc 127.0.0.1:13389]        [RDP 127.0.0.1:3389]
     本地入口                              本地目标服务
```

打洞成功则双方 P2P 直连；失败则 A↔Server↔B 走中继，对上层应用无感。

## 目录结构

```
UDP_tunnel_demo/
├── server/             # 公网服务端（Rendezvous + STUN + TURN）
├── client/             # 客户端
├── internal/
│   ├── protocol/       # JSON 控制协议
│   ├── tunnel/         # KCP 可靠传输封装
│   └── forward/        # smux 多路复用 + TCP 端口转发
└── build-all.bat       # 跨平台构建脚本
```

## 构建

Windows 上执行：

```bat
build-all.bat
```

产出：
- `dist/server` — Linux amd64（可直接在 Debian/Ubuntu 运行）
- `dist/client.exe` — Windows amd64

## 部署

### 1. 服务端（公网 VPS）

把 `dist/server` 上传到服务器，放行以下端口：

| 端口 | 协议 | 用途 |
|---|---|---|
| 7000 | UDP | Rendezvous + STUN + TURN 主端口 |
| 7002 | UDP | STUN 备用端口（NAT 类型探测用） |
| 7001 | TCP | HTTP 健康检查 |

启动：

```bash
nohup ./server -listen :7000 -stun-alt :7002 -http :7001 > server.log 2>&1 &
```

验证：`curl http://<公网IP>:7001/health`

### 2. 客户端（两端各一个）

**最简用法（远程桌面转发）：**

出口端 B（RDP 被连接方，无需任何转发规则）：

```bat
client.exe -server <公网IP>:7000 -id B -peer A
```

入口端 A（要用 RDP 的一方，把本地 13389 映射到 B 上的 3389）：

```bat
client.exe -server <公网IP>:7000 -id A -peer B -forward 13389:127.0.0.1:3389
```

然后在 A 机器上打开 mstsc，输入 `127.0.0.1:13389` 即可。

### 3. 常用命令

**多服务同时转发：**
```bat
client.exe -server <公网IP>:7000 -id A -peer B ^
  -forward 13389:127.0.0.1:3389 ^
  -forward 12222:127.0.0.1:22 ^
  -forward 18080:127.0.0.1:80
```

**NAT 类型探测（诊断）：**
```bat
client.exe -server <公网IP>:7000 -probe
```

**强制走 TURN 中继（测试/排障）：**
```bat
client.exe -server <公网IP>:7000 -id A -peer B -force-relay -forward 13389:127.0.0.1:3389
```

## 参数参考

### 服务端

| 参数 | 默认 | 说明 |
|---|---|---|
| `-listen` | `:7000` | UDP 主端口 |
| `-stun-alt` | `:7002` | UDP 备用端口（STUN 探测） |
| `-http` | `:7001` | HTTP 健康检查端口 |

### 客户端

| 参数 | 默认 | 说明 |
|---|---|---|
| `-server` | — | 公网服务器地址 `ip:port` |
| `-id` | — | 本端 ID |
| `-peer` | — | 对端 ID |
| `-forward` | — | 转发规则 `LOCAL:HOST:PORT`，可重复 |
| `-punch-timeout` | `15s` | 打洞超时，超时后切 TURN |
| `-force-relay` | `false` | 跳过打洞直接走中继 |
| `-probe` | `false` | NAT 类型探测模式 |
| `-alt-port` | `7002` | 服务端 STUN 备用端口 |

### 服务端 HTTP 接口

| 路径 | 说明 |
|---|---|
| `/` | 欢迎页 |
| `/health` | JSON 健康数据（运行时长、注册/配对数、中继字节数） |
| `/peers` | 当前已注册客户端列表 |

## 连通性分级

| 场景 | 表现 | 带宽消耗 |
|---|---|---|
| 双方 Cone NAT | P2P 直连（`🎯`） | 零（直接互通） |
| 一方对称 NAT | 视情况，多数仍可打通 | 零或少量中继 |
| 双方对称 / CGNAT | 自动走 TURN 中继（`🔁`） | 全流量过服务器 |

## 已知限制

- 无加密（KCP 原生不加密，需要可以自己套一层，或上 QUIC）
- 无 UPnP 主动开映射
- 无对称 NAT 端口预测
- 服务端已注册客户端无过期回收
- 只支持 IPv4

## 许可证

Demo 项目，随意使用。
