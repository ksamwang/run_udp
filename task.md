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

## 已确认产品决策

- 默认网段：`172.16.10.0/24`，由服务端配置并通过 LAN bootstrap 下发。
- 设备 ID：LAN 和当前端口转发共用同一个设备 ID。
- LAN 密钥：使用独立设备密钥，私钥保存在本地，公钥保存在服务端。
- 隔离模型：组即虚拟网络，一个组一个网段。
- 多网络：数据模型支持多虚拟网络，第一版 UI 只开放一个默认网络。
- 验收优先级：直接以 TCP/RDP 验收，最终以 RDP 可用作为产品验收标准。

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

验收：

- Admin API 使用 JWT。
- LAN Agent API 使用独立设备认证方案，不长期依赖部署级 PSK。
- API 错误响应继续使用统一 JSON contract。

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

## 阶段 4：Wintun PoC

- [ ] 研究 Wintun 分发方式。
- [ ] `internal/wintun` 封装创建/打开适配器。
- [ ] 设置虚拟 IP。
- [ ] 设置 MTU，初始建议 `1280`。
- [ ] 添加 `172.16.10.0/24` 路由。
- [ ] 从 TUN 读取 IPv4 包。
- [ ] 向 TUN 写入 IPv4 包。

验收：

- 单机上能创建 `UDP Tunnel LAN` 虚拟网卡。
- 程序退出后能清理路由和资源。

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
- [ ] 统计 tx/rx bytes、drop reason。

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

验收：

- P2P 模式下 A 能访问 B 的 TCP 服务。
- relay 模式下 A 能访问 B 的 TCP 服务。

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
- [ ] 设备虚拟 IP 展示和编辑。
- [ ] ACL 规则页面。
- [ ] LAN 状态页面：
  - 虚拟网卡状态
  - peer path
  - tx/rx
  - last handshake
  - last error

验收：

- 不影响现有设备、规则、会话页面。
- LAN 菜单可独立关闭或标记实验功能。

## 阶段 9：安装和发布

- [ ] 构建 `UDPTunnelLAN.exe`。
- [ ] 独立 Windows 服务安装。
- [ ] 独立托盘。
- [ ] 安装包是否与当前客户端合并，需要后续确认。
- [ ] Release 资产加入 LAN 客户端。

待确认：

- `UDPTunnelLAN` 是独立安装包，还是和当前客户端合并安装？
- LAN 服务默认是否开机启动？
- LAN 功能是否默认启用？

## 阶段 10：后续增强

- [ ] Magic DNS。
- [ ] 多设备组/多虚拟网络 UI。
- [ ] 子网路由。
- [ ] LAN 内流量审计。
- [ ] IPv6。
- [ ] 移动端支持。

## 当前未决问题

- `UDPTunnelLAN` 是独立安装包，还是和当前客户端合并安装？
- LAN 服务默认是否开机启动？
- LAN 功能是否默认启用？
