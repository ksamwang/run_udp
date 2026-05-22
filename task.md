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
- 隔离模型：组即虚拟网络，一个组一个网段。
- 多网络：数据模型支持多虚拟网络，第一版 UI 只开放一个默认网络。
- 验收优先级：直接以 TCP/RDP 验收，最终以 RDP 可用作为产品验收标准。
- 安装形态：`UDPTunnelLAN` 使用独立安装包。
- 默认启动：LAN 服务默认开机启动。
- 默认启用：LAN 功能默认启用。

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
- [ ] `build-all.bat` 构建 `UDPTunnelLAN.exe`。
- [ ] `release.bat` 上传 `UDPTunnelLAN.exe` 或独立 zip。
- [ ] 增加 `lan.json.example`，只包含本地最小引导配置。
- [ ] 明确 Windows 服务名：`UDPTunnelLAN`。
- [ ] 明确托盘名：`UDP Tunnel LAN`。

验收：

- `go test ./...` 通过。
- `GOOS=windows GOARCH=amd64 go build ./cmd/UDPTunnelLAN` 通过。

## 阶段 1：控制面数据模型

- [ ] 新增虚拟网络模型 `virtual_networks`：
  - `id`
  - `name`
  - `cidr`
  - `enabled`
  - `created_at`
  - `updated_at`
- [ ] 新增虚拟地址模型 `virtual_addresses`：
  - `device_id`
  - `network_id`
  - `virtual_ip`
  - `hostname`
- [ ] 新增虚拟 ACL 模型 `virtual_acl_rules`：
  - `source_device_id`
  - `source_group_id`
  - `target_device_id`
  - `target_group_id`
  - `protocol`
  - `port_start`
  - `port_end`
  - `action`
  - `enabled`
- [ ] 新增虚拟路由模型 `virtual_routes`：
  - `device_id`
  - `network_id`
  - `cidr`
  - `advertise`
  - `accept`
- [ ] 新增虚拟会话/状态模型 `virtual_sessions` 或 `virtual_peer_states`。
- [ ] 默认创建一个虚拟网络，CIDR 为 `172.16.10.0/24`。
- [ ] 数据模型支持多个虚拟网络，但第一版业务默认只使用一个网络。

验收：

- MySQL 5.5 兼容。
- Gorm AutoMigrate 正常。
- store 层单元测试覆盖增删改查和唯一约束。

## 阶段 2：服务端 LAN API

- [ ] `POST /api/lan/bootstrap`
- [ ] `GET /api/admin/lan/networks`
- [ ] `POST /api/admin/lan/networks`
- [ ] `PATCH /api/admin/lan/networks/{id}`
- [ ] `DELETE /api/admin/lan/networks/{id}`
- [ ] `GET /api/admin/lan/addresses`
- [ ] `PATCH /api/admin/lan/addresses/{device_id}`
- [ ] `GET /api/admin/lan/acl`
- [ ] `POST /api/admin/lan/acl`
- [ ] `PATCH /api/admin/lan/acl/{id}`
- [ ] `DELETE /api/admin/lan/acl/{id}`
- [ ] `POST /api/lan/status`
- [ ] LAN bootstrap 返回协议版本 `version` 和客户端能力 `capabilities`。
- [ ] LAN bootstrap 返回服务端配置版本，便于客户端判断是否需要刷新网络、ACL、peer 信息。

验收：

- Admin API 使用 JWT。
- LAN Agent API 使用独立设备认证方案，不长期依赖部署级 PSK。
- API 错误响应继续使用统一 JSON contract。
- 旧 LAN 客户端遇到不兼容 bootstrap 响应时能给出明确错误，而不是静默失败。

## 阶段 3：设备身份和密钥

- [ ] `UDPTunnelLAN` 首次启动生成设备私钥。
- [ ] 服务端保存设备公钥。
- [ ] LAN 使用当前端口转发产品线已有设备 ID 作为设备主键。
- [ ] bootstrap 返回网络配置、虚拟 IP、路由表、ACL、peer 公钥。
- [ ] 设计设备密钥轮换流程。
- [ ] 明确遗失密钥后的重新绑定流程。

验收：

- 单设备泄露不影响整个部署。
- 不再把部署级 PSK 作为 LAN 数据面的主要安全边界。

## 阶段 3.5：LAN 安全协议设计

- [ ] 明确设备私钥算法和公钥格式。
- [ ] 设计 peer 握手流程。
- [ ] 设计会话密钥派生流程。
- [ ] 设计 packet frame 加密和认证格式。
- [ ] 增加 nonce、sequence 或时间窗口，防止重放。
- [ ] 明确 key rotation 后新旧 session 的兼容和失效策略。
- [ ] 明确 ACL 在发送端、接收端、服务端控制面的执行边界。

验收：

- 数据面 packet 不以明文裸传。
- 接收端必须二次校验来源设备、目标设备、网络 ID 和 ACL。
- 重放 packet 不应被接收端接受。

## 阶段 4：Wintun PoC

- [ ] 研究 Wintun 分发方式。
- [ ] 明确 Wintun DLL/驱动打包方式、版本、许可证、校验。
- [ ] 明确创建虚拟网卡、设置 IP、设置路由所需的管理员权限模型。
- [ ] 明确 LAN Service 和托盘之间的职责边界：Service 负责虚拟网卡和数据面，托盘只做状态展示和控制入口。
- [ ] `internal/wintun` 封装创建/打开适配器。
- [ ] 设置虚拟 IP。
- [ ] 设置 MTU，初始建议 `1280`。
- [ ] 添加 `172.16.10.0/24` 路由。
- [ ] 从 TUN 读取 IPv4 包。
- [ ] 向 TUN 写入 IPv4 包。

验收：

- 单机上能创建 `UDP Tunnel LAN` 虚拟网卡。
- 程序退出后能清理路由和资源。
- 普通用户启动托盘时不会尝试直接创建网卡或修改系统路由。

## 阶段 4.5：路由、MTU 和系统状态处理

- [ ] 检测本机路由表是否和虚拟网段冲突。
- [ ] 冲突时上报服务端，并在管理后台展示冲突状态。
- [ ] 明确虚拟网段变更后的客户端路由更新策略。
- [ ] 明确 MTU、MSS clamp、IPv4 分片和超大 packet 的处理策略。
- [ ] 明确系统睡眠唤醒、网络切换、网卡禁用/启用后的恢复流程。
- [ ] 明确异常退出、服务停止、卸载时的路由和虚拟网卡清理流程。

验收：

- 发现 `172.16.10.0/24` 路由冲突时不应静默覆盖用户现有路由。
- RDP 长连接在合理网络抖动后可重新建立。
- 卸载后不残留 LAN 服务、托盘自启动、虚拟网卡或虚拟路由。

## 阶段 5：Packet Router

- [ ] 解析 IPv4 header。
- [ ] 根据目标虚拟 IP 查找目标设备。
- [ ] 根据 ACL 判断是否允许转发。
- [ ] 封装 packet frame：
  - `network_id`
  - `src_device`
  - `dst_device`
  - `packet_type`
  - `payload`
- [ ] 优先处理 TCP 转发所需的 IPv4 包路径。
- [ ] ICMP 可作为调试能力，但不作为第一版核心验收目标。
- [ ] UDP 留作后续能力。
- [ ] 统计 tx/rx bytes、packet count、drop reason。
- [ ] 统计 ACL deny、route miss、MTU drop、peer unavailable。

验收：

- A 能通过虚拟 IP 建立到 B 的 TCP 连接。
- 被 ACL 拒绝的包可计数和上报。

## 阶段 6：P2P/Relay Packet Link

- [ ] 复用当前 NAT 探测和 rendezvous。
- [ ] 为 LAN 数据面设计 packet mode。
- [ ] P2P 可用时直连。
- [ ] P2P 不可用时走 relay。
- [ ] 支持 peer session keepalive。
- [ ] 网络变化时重建 peer link。
- [ ] 增加每设备最大 peer session 数限制。
- [ ] 增加 peer session idle timeout 和资源清理。
- [ ] 增加 relay 流量统计和限流预留点。

验收：

- P2P 模式下 A 能访问 B 的 TCP 服务。
- relay 模式下 A 能访问 B 的 TCP 服务。
- peer session 泄漏不会导致客户端或服务端资源持续增长。

## 阶段 7：TCP/RDP 可用性

- [ ] 验证 RDP：`mstsc /v:172.16.10.x`
- [ ] 验证 SSH。
- [ ] 验证 HTTP。
- [ ] 验证长连接稳定性。
- [ ] 验证网络切换后的恢复。

验收：

- A 访问 B 的 `3389` 可用。
- A 能使用 `mstsc /v:172.16.10.x` 连接 B。
- 中断恢复后应用层可重新连接。

## 阶段 8：管理后台

- [ ] 新增菜单：虚拟局域网。
- [ ] 网络列表。
- [ ] 默认网络配置页，支持查看和修改默认 CIDR。
- [ ] 路由冲突、密钥异常、bootstrap 失败等错误状态展示。
- [ ] 设备虚拟 IP 展示和编辑。
- [ ] ACL 规则页面。
- [ ] LAN 状态页面：
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

- [ ] 构建 `UDPTunnelLAN.exe`。
- [ ] 独立 Windows 服务安装。
- [ ] 独立托盘。
- [ ] 独立安装包，不与当前端口转发客户端安装包合并。
- [ ] LAN 服务安装后默认开机启动。
- [ ] LAN 功能安装后默认启用。
- [ ] Release 资产加入 LAN 客户端。
- [ ] 明确安装目录、日志目录、配置目录，不与当前端口转发客户端冲突。
- [ ] 明确 LAN 客户端和服务端 LAN API 的版本兼容策略。
- [ ] 增加 Windows 重启后服务自启动验收。

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

- 设备私钥算法使用 Ed25519、X25519，还是复用现有安全帧里的算法？
- LAN 数据面 packet 加密是直接复用 `internal/secure`，还是单独设计 packet session crypto？
- 是否要在第一版实现 MSS clamp，还是先通过固定 MTU 和 drop 日志处理？
- 默认虚拟网段冲突时，是自动换网段，还是只上报冲突并要求管理员手动修改？
- 一个设备未来是否允许同时加入多个虚拟网络？
- 第一版是否需要提供 Magic DNS 的数据模型预留，但 UI 暂不开放？
- ACL 默认策略是默认拒绝，还是默认允许同组设备互通？
