# UDP Tunnel Virtual LAN Task Plan

## 目标

在不影响当前端口转发产品线的前提下，新增一条真正虚拟局域网产品线。

当前产品线继续由 `cmd/client` 承载，聚焦端口转发、规则、P2P/relay。

新产品线由 `cmd/UDPTunnelLAN` 承载，目标是基于虚拟 IP 和虚拟网卡实现设备间三层互联。

## 边界原则

- 不改动当前端口转发核心逻辑。
- `cmd/UDPTunnelLAN` 不直接复用 `cmd/client/main.go` 内部函数。
- 只复用稳定的 `internal` 底层模块，如配置、协议、安全帧、NAT 探测、P2P/relay 能力。
- 虚拟局域网的数据模型、API、前端菜单和客户端运行时独立演进。
- 初期只支持 Windows。
- 初期只做 IPv4。
- 第一版不承诺广播、多播、NetBIOS 自动发现能力，以 TCP/RDP 可用为主。

## 已确认产品决策

- 默认网段：`172.16.10.0/24`，由服务端配置并通过 LAN bootstrap 下发。
- 设备 ID：LAN 和当前端口转发共用同一个设备 ID。
- LAN 密钥：使用独立设备密钥，私钥保存在本地，公钥保存在服务端。
- LAN 设备身份密钥使用 Ed25519，用于设备身份签名和服务端公钥登记。
- LAN peer 会话密钥使用 X25519 临时密钥协商，通过 HKDF 派生 packet session key。
- LAN 数据面加密单独设计 packet session crypto，不直接复用当前基于部署级 PSK 的 `internal/secure.Codec`。
- LAN packet AEAD 优先使用 ChaCha20-Poly1305，复用现有安全帧的加密算法思路，不复用其 PSK 边界。
- 隔离模型：组即虚拟网络，一个组一个网段。
- 多网络：数据模型支持多虚拟网络，第一版 UI 只开放一个默认网络。
- 一个设备允许同时加入多个虚拟网络，所有地址、路由、ACL、peer session 必须按 `network_id` 隔离。
- 验收优先级：直接以 TCP/RDP 验收，最终以 RDP 可用作为产品验收标准。
- 第一版实现 TCP MSS clamp，配合固定 TUN MTU 降低 RDP 黑洞和分片风险。
- 默认虚拟网段冲突时由服务端自动分配新的可用网段，并通过 LAN bootstrap 下发。
- 第一版预留 Magic DNS 数据模型，管理后台 UI 暂不开放。
- ACL 默认策略为同组设备互通，显式拒绝规则优先级高于默认允许。
- 安装形态：`UDPTunnelLAN` 使用独立安装包。
- 默认启动：LAN 服务默认开机启动。
- 默认启用：LAN 功能默认启用。
- 服务端默认不允许中继，LAN relay 必须由管理员显式开启。

## 建议目录

```text
cmd/
  UDPTunnelLAN/
    main.go

internal/
  lan/
    app.go
    bootstrap.go
    config.go
    service.go
    tray.go

  vnet/
    network.go
    address.go
    route.go
    acl.go
    status.go

  wintun/
    adapter_windows.go
    adapter_stub.go

  packet/
    router.go
    codec.go
    peer.go
    acl.go
```

## 阶段 0：产品线骨架

- [x] 新增 `cmd/UDPTunnelLAN` 最小入口。
- [x] `build-all.bat` 构建 `UDPTunnelLAN.exe`。
- [x] `release.bat` 上传 `UDPTunnelLAN.exe` 或独立 zip。
- [x] 增加 `lan.json.example`，只包含本地最小引导配置。
- [x] 明确 Windows 服务名：`UDPTunnelLAN`。
- [x] 明确托盘名：`UDP Tunnel LAN`。

验收：

- `go test ./...` 通过。
- `GOOS=windows GOARCH=amd64 go build ./cmd/UDPTunnelLAN` 通过。

## 阶段 1：控制面数据模型

- [x] 新增虚拟网络模型 `virtual_networks`：
  - `id`
  - `name`
  - `cidr`
  - `enabled`
  - `created_at`
  - `updated_at`
- [x] 新增虚拟地址模型 `virtual_addresses`：
  - `device_id`
  - `network_id`
  - `virtual_ip`
  - `hostname`
  - `dns_enabled`
- [x] 新增虚拟 ACL 模型 `virtual_acl_rules`：
  - `source_device_id`
  - `source_group_id`
  - `target_device_id`
  - `target_group_id`
  - `protocol`
  - `port_start`
  - `port_end`
  - `action`
  - `enabled`
- [x] 新增虚拟路由模型 `virtual_routes`：
  - `device_id`
  - `network_id`
  - `cidr`
  - `advertise`
  - `accept`
- [x] 新增虚拟会话/状态模型 `virtual_sessions` 或 `virtual_peer_states`。
- [x] 默认创建一个虚拟网络，CIDR 为 `172.16.10.0/24`。
- [x] 数据模型支持多个虚拟网络，但第一版业务默认只使用一个网络。
- [x] 预留 Magic DNS 所需的 `hostname`、`dns_enabled`、网络内唯一约束。

验收：

- MySQL 5.5 兼容。
- Gorm AutoMigrate 正常。
- store 层单元测试覆盖增删改查和唯一约束。
- 同一个设备可拥有多个 `network_id` 下的虚拟地址。
- 同一个虚拟网络内 `virtual_ip` 和 `hostname` 不允许重复。

## 阶段 2：服务端 LAN API

- [x] `POST /api/lan/bootstrap`
- [x] `GET /api/admin/lan/networks`
- [x] `POST /api/admin/lan/networks`
- [x] `PATCH /api/admin/lan/networks/{id}`
- [x] `DELETE /api/admin/lan/networks/{id}`
- [x] `GET /api/admin/lan/addresses`
- [x] `PATCH /api/admin/lan/addresses/{device_id}`
- [x] `GET /api/admin/lan/acl`
- [x] `POST /api/admin/lan/acl`
- [x] `PATCH /api/admin/lan/acl/{id}`
- [x] `DELETE /api/admin/lan/acl/{id}`
- [x] `POST /api/lan/status`
- [x] LAN bootstrap 返回协议版本 `version` 和客户端能力 `capabilities`。
- [x] LAN bootstrap 返回服务端配置版本，便于客户端判断是否需要刷新网络、ACL、peer 信息。

验收：

- Admin API 使用 JWT。
- LAN Agent API 使用独立设备认证方案，不长期依赖部署级 PSK。
- API 错误响应继续使用统一 JSON contract。
- 旧 LAN 客户端遇到不兼容 bootstrap 响应时能给出明确错误，而不是静默失败。

## 阶段 3：设备身份和密钥

- [x] `UDPTunnelLAN` 首次启动生成设备私钥。
- [x] 服务端保存设备公钥。
- [x] LAN 使用当前端口转发产品线已有设备 ID 作为设备主键。
- [x] bootstrap 返回网络配置、虚拟 IP、路由表、ACL、peer 公钥。
- [x] 设计设备密钥轮换流程。
- [x] 明确遗失密钥后的重新绑定流程。

说明：

- 第一版设备身份密钥使用 Ed25519，本地私钥保存为 `lan-identity.json`，公钥通过 LAN bootstrap 上报服务端。
- 第一版密钥轮换采用“客户端生成新本地私钥后重新 bootstrap 覆盖服务端公钥”的流程。
- 第一版遗失密钥后的重新绑定流程与轮换一致；后续安全协议阶段再增加签名挑战、管理员确认或重绑审计。

验收：

- 单设备泄露不影响整个部署。
- 不再把部署级 PSK 作为 LAN 数据面的主要安全边界。

## 阶段 3.5：LAN 安全协议设计

- [x] 设备身份密钥使用 Ed25519，明确私钥本地存储格式和公钥上报格式。
- [x] peer 会话使用 X25519 临时密钥协商。
- [x] 设计 peer 握手流程。
- [x] 设计会话密钥派生流程，使用 HKDF 派生方向独立的 tx/rx key。
- [x] 设计 packet frame 加密和认证格式，使用独立 LAN packet session crypto。
- [x] packet AEAD 优先使用 ChaCha20-Poly1305。
- [x] 增加 nonce、sequence 或时间窗口，防止重放。
- [x] 明确 key rotation 后新旧 session 的兼容和失效策略。
- [x] 明确 ACL 在发送端、接收端、服务端控制面的执行边界。
- [x] ACL 默认允许同组互通，显式拒绝规则优先。

说明：

- 协议设计记录在 `docs/lan-security-protocol.md`。
- 当前实现边界为 packet session frame、HKDF 方向密钥派生、ChaCha20-Poly1305 加密认证、sequence replay window。
- X25519 握手消息结构和 transcript 已设计，实际 peer 握手传输接入放到 P2P/Relay Packet Link 阶段。

验收：

- 数据面 packet 不以明文裸传。
- 接收端必须二次校验来源设备、目标设备、网络 ID 和 ACL。
- 重放 packet 不应被接收端接受。
- 不依赖部署级 PSK 作为 LAN packet 数据面安全边界。

## 阶段 4：Wintun PoC

- [x] 研究 Wintun 分发方式。
- [x] 明确 Wintun DLL/驱动打包方式、版本、许可证、校验。
- [x] 明确创建虚拟网卡、设置 IP、设置路由所需的管理员权限模型。
- [x] 明确 LAN Service 和托盘之间的职责边界：Service 负责虚拟网卡和数据面，托盘只做状态展示和控制入口。
- [x] `internal/wintun` 封装创建/打开适配器。
- [x] 设置虚拟 IP。
- [x] 设置 MTU，初始建议 `1280`。
- [x] 添加 `172.16.10.0/24` 路由。
- [x] 从 TUN 读取 IPv4 包。
- [x] 向 TUN 写入 IPv4 包。

说明：

- PoC 使用官方 `golang.zx2c4.com/wintun` Go 模块，避免手写 DLL 调用。
- Windows 网络配置 PoC 使用隐藏窗口 `netsh` 设置 IPv4、MTU 和路由。
- 创建虚拟网卡、设置 IP/MTU/路由需要管理员权限；后续正式运行时由 Windows Service 执行，托盘只做 UI/控制入口。
- `UDPTunnelLAN -wintun-poc` 可创建/打开 `UDP Tunnel LAN` 适配器并配置 `172.16.10.0/24` PoC 路由。

验收：

- 单机上能创建 `UDP Tunnel LAN` 虚拟网卡。
- 程序退出后能清理路由和资源。
- 普通用户启动托盘时不会尝试直接创建网卡或修改系统路由。

## 阶段 4.5：路由、MTU 和系统状态处理

- [x] 检测本机路由表是否和虚拟网段冲突。
- [x] 冲突时上报服务端，并在管理后台展示冲突状态。
- [x] 服务端在默认网段冲突时自动分配新的可用网段。
- [x] 明确虚拟网段自动变更后的客户端路由更新策略。
- [x] 实现 TCP MSS clamp，第一版建议 clamp 到不超过 `1200`。
- [x] 明确 MTU、IPv4 分片和超大 packet 的处理策略。
- [x] 明确系统睡眠唤醒、网络切换、网卡禁用/启用后的恢复流程。
- [x] 明确异常退出、服务停止、卸载时的路由和虚拟网卡清理流程。

说明：

- 路由和 MTU 策略记录在 `docs/lan-routing-mtu.md`。
- 当前实现包含本机路由检测、候选网段选择、MSS 计算策略、状态上报字段和 PoC 路由清理入口。
- TCP SYN MSS 实际重写放到 Packet Router 阶段实现，因为需要 IPv4/TCP 包解析。
- 睡眠唤醒、网络切换和服务生命周期事件订阅放到后续 LAN runtime/service 阶段接入。

验收：

- 发现 `172.16.10.0/24` 路由冲突时不应静默覆盖用户现有路由。
- 路由冲突后客户端应使用服务端重新下发的可用网段恢复 LAN。
- TCP SYN 包的 MSS 能按 LAN MTU 策略被正确限制。
- RDP 长连接在合理网络抖动后可重新建立。
- 卸载后不残留 LAN 服务、托盘自启动、虚拟网卡或虚拟路由。

## 阶段 5：Packet Router

- [x] 解析 IPv4 header。
- [x] 根据目标虚拟 IP 查找目标设备。
- [x] 根据 ACL 判断是否允许转发。
- [x] 封装 packet frame：
  - `network_id`
  - `src_device`
  - `dst_device`
  - `packet_type`
  - `payload`
- [x] 优先处理 TCP 转发所需的 IPv4 包路径。
- [x] ICMP 可作为调试能力，但不作为第一版核心验收目标。
- [x] UDP 留作后续能力。
- [x] 统计 tx/rx bytes、packet count、drop reason。
- [x] 统计 ACL deny、route miss、MTU drop、peer unavailable。

验收：

- A 能通过虚拟 IP 建立到 B 的 TCP 连接。
- 被 ACL 拒绝的包可计数和上报。

## 阶段 6：P2P/Relay Packet Link

- [x] 复用当前 NAT 探测和 rendezvous。
- [x] 为 LAN 数据面设计 packet mode。
- [x] P2P 可用时直连。
- [x] P2P 不可用且服务端允许中继时走 relay。
- [x] 支持 peer session keepalive。
- [x] 网络变化时重建 peer link。
- [x] 增加每设备最大 peer session 数限制。
- [x] 增加 peer session idle timeout 和资源清理。
- [x] 增加 relay 流量统计和限流预留点。

实现说明：

- LAN 数据面使用 `lan-packet` profile 复用现有 rendezvous 和 UDP relay 通道。
- 当前完成 packet link 会话管理、HTTP relay API 门控和 UDP P2P packet link 接入；服务端默认关闭 relay。
- 当前 UDP P2P packet 使用 LAN register 携带的 X25519 临时公钥协商 packet session key，register 由 Ed25519 设备身份签名保护。

验收：

- P2P 模式下 A 能访问 B 的 TCP 服务。
- relay 模式下 A 能访问 B 的 TCP 服务，且默认关闭 relay 时应明确失败并上报状态。
- peer session 泄漏不会导致客户端或服务端资源持续增长。

## 阶段 7：TCP/RDP 可用性

- [x] Wintun ReadPacket 接入 Packet Router。
- [x] Packet Router 输出接入 LAN relay send API。
- [x] LAN relay poll 写回 Wintun WritePacket。
- [x] 服务端 LAN relay API 受 `allow_relay` 控制，默认关闭。
- [x] UDP P2P packet link 端到端接入。
- [x] UDP P2P packet link 使用 X25519 临时密钥协商和 packet session crypto。
- [ ] 验证 RDP：`mstsc /v:172.16.10.x`
- [ ] 验证 SSH。
- [ ] 验证 HTTP。
- [ ] 验证长连接稳定性。
- [ ] 验证网络切换后的恢复。

验收：

- A 访问 B 的 `3389` 可用。
- A 能使用 `mstsc /v:172.16.10.x` 连接 B。
- 中断恢复后应用层可重新连接。

## 阶段 7.5：失败后重开 socket 并重新注册/打洞

目标：

- `UDPTunnelLAN` 仍然保持全局只有一个 LAN UDP socket，不做多个 socket 或 ICE candidate。
- 当当前 UDP NAT 映射长期无法建立可用 KCP tunnel 时，关闭旧 socket，重开新 socket，获得新的本地端口和公网映射。
- socket 重开后重新注册所有 peer，触发服务端下发新的 `peer_info`，重新走 direct punch 和 relay fallback。
- 不影响当前端口转发产品线，不改 `cmd/client`。

触发条件：

- [x] relay mode 下 KCP open/handshake 连续失败达到阈值，第一版建议 3 次。
- [x] relay mode 进入后持续超过超时时间仍没有可用 tunnel，第一版建议 60 秒。
- [x] direct punch 失败只先进入现有 relay fallback，不立即重开 socket。
- [x] 已经 `KCP tunnel ready` 的 peer 不主动触发 socket rotation，避免打断可用连接。
- [x] socket rotation 设置冷却时间，第一版建议 60 秒内最多一次，防止网络抖动时频繁轮换。

开发任务：

- [x] 为 `lanP2P` 增加 socket 生命周期管理：
  - `connMu` 或等价锁保护当前 `*net.UDPConn`。
  - 所有控制包发送统一通过当前 socket。
  - `readLoop` 能在旧 socket 关闭后退出，并由 rotation 启动新的 readLoop。
  - rotation 期间不允许同时存在两个可用 LAN socket。
- [x] 为 peer 增加建链失败状态：
  - 记录连续 KCP open/handshake 失败次数。
  - KCP tunnel ready 后清零失败次数。
  - 记录最近一次进入 relay 的时间。
  - 记录最近一次 socket rotation 的时间。
- [x] 实现 `rotateSocketAndRestartPunch(reason)`：
  - 关闭旧 UDP socket。
  - 创建新的 UDP socket，端口仍由系统分配。
  - 重新尝试 UPnP 映射。
  - 更新 `lanP2P.conn`。
  - 重启 P2P readLoop。
  - 清理全部 peer 的 `PacketConn`、KCP conn 和建链状态。
  - 重启所有 peer 的注册和 relay fallback timer。
- [x] 实现 peer 状态清理：
  - `connected=false`
  - `punched=false`
  - `punching=false`
  - `isRelay=false`
  - `pc=nil`
  - `kcp=nil`
  - 清理 open retry / relay timer 的旧状态。
- [x] 让已有 registerLoop 在 socket rotation 后继续使用新 socket，或显式重启 registerLoop，避免服务端继续保留旧公网端口。
- [x] socket rotation 后立即向服务端重新发送 LAN register，而不是等待下一个 3 秒 tick。
- [x] socket rotation 后重新接受服务端下发的 `peer_info`，不应因为旧 relay mode 状态继续忽略新的 direct 地址。
- [x] 日志增加明确状态：
  - `LAN P2P rotating UDP socket: reason=... old=...`
  - `LAN P2P socket rotated: new=... upnp=...`
  - `LAN P2P peer reset after socket rotation: peer=...`
  - `LAN P2P re-registering peers after socket rotation: peers=...`

测试任务：

- [x] relay KCP open 连续失败达到阈值后会触发 socket rotation。
- [x] socket rotation 后 peer 状态全部回到 direct punch 初始状态。
- [x] socket rotation 后旧 `PacketConn` 被丢弃，新 `PacketConn` 使用新 socket。
- [x] socket rotation 后 register 使用新 socket 发出，服务端可看到新的来源端口。
- [x] socket rotation 冷却时间生效，短时间内不会反复轮换。
- [x] 已经 ready 的 KCP tunnel 不会被无关 peer 的失败触发误关闭。

验收：

- 日志中能看到 socket 从旧端口切换到新端口。
- 服务端能收到来自新公网端口的 LAN register。
- 对端能收到新的 `peer_info` 并重新打洞。
- 在某个 NAT 映射卡死时，客户端能自动换端口重试，而不是永久停在旧端口和 relay mode。
- 不引入多个并发 LAN UDP socket。

## 阶段 8：管理后台

- [x] 新增菜单：虚拟局域网。
- [x] 网络列表。
- [x] 默认网络配置页，支持查看和修改默认 CIDR。
- [x] 路由冲突、密钥异常、bootstrap 失败等错误状态展示。
- [x] 设备虚拟 IP 展示和编辑。
- [x] ACL 规则页面。
- [x] LAN 状态页面：
  - 虚拟网卡状态
  - peer path
  - RTT/loss
  - tx/rx
  - drop reason
  - last handshake
  - last error

验收：

- 不影响现有设备、规则、会话页面。
- LAN 菜单可独立关闭或标记实验功能。

## 阶段 9：安装和发布

- [x] 构建 `UDPTunnelLAN.exe`。
- [x] 独立 Windows 服务安装。
- [x] 独立托盘。
- [x] 独立安装包，不与当前端口转发客户端安装包合并。
- [x] LAN 服务安装后默认开机启动。
- [x] LAN 功能安装后默认启用。
- [x] Release 资产加入 LAN 客户端。
- [x] 明确安装目录、日志目录、配置目录，不与当前端口转发客户端冲突。
- [x] 明确 LAN 客户端和服务端 LAN API 的版本兼容策略。
- [x] 增加 Windows 重启后服务自启动验收。

验收：

- LAN 安装包和当前客户端安装包可同时安装、同时卸载，互不破坏。
- 服务启动失败时托盘和日志能展示明确原因。

## 阶段 9.5：测试矩阵和验收环境

- [ ] Windows 10 验收。
- [ ] Windows 11 验收。
- [ ] 管理员安装、普通用户登录场景验收。
- [ ] 同局域网 P2P 验收。
- [ ] 不同 NAT P2P 验收。
- [ ] relay-only 验收。
- [ ] 路由冲突场景验收。
- [ ] 睡眠唤醒场景验收。
- [ ] 网络切换场景验收。
- [ ] RDP 长连接场景验收。
- [ ] 卸载清理场景验收。

验收：

- 第一版发布前至少完成 Windows 10、Windows 11、relay-only、RDP、卸载清理五类核心验收。

## 阶段 10：后续增强

- [ ] Magic DNS。
- [ ] 多设备组/多虚拟网络 UI。
- [ ] 子网路由。
- [ ] LAN 内流量审计。
- [ ] IPv6。
- [ ] 移动端支持。

## 当前未决问题

- 暂无。
