# UDPTunnelLAN 网络优化任务

本文档作为后续优化 `cmd/UDPTunnelLAN` 网络性能的执行参照。目标是让 UDPTunnelLAN 在可比场景下尽量接近老 Agent 的延迟、吞吐和稳定性。完成某项后，把对应的 `- [ ]` 改为 `- [x]`。

## 产品边界

- [ ] 不修改 `cmd/client/**` 老 Agent 产品线。
- [ ] 不改变老 Agent 的配置格式、服务名、更新逻辑、端口转发行为和默认网络语义。
- [ ] 共享代码如需调整，必须保持老 Agent 行为兼容；无法确认兼容时，优先为 UDPTunnelLAN 单独新增实现。
- [ ] UDPTunnelLAN 优化优先放在 `cmd/UDPTunnelLAN/**`、`internal/lantransport/**`、`internal/packet/**`、`internal/wintun/**`、`internal/vnet/**`。
- [ ] 服务端改动限定在 LAN 分支：`/api/lan/*`、`handleLANRegister`、`lanPeers`、LAN packet relay、LAN 状态和 LAN 管理后台。
- [ ] 每轮改动后执行测试，并按用户要求提交 git，避免中间成果丢失。

## 性能目标

- [ ] P2P 可达时，交互延迟优先接近 Agent interactive profile。
- [ ] 大文件或持续吞吐场景，尽量接近 Agent bulk profile 的吞吐能力。
- [ ] relay 场景不能只满足“能通”，需要降低 HTTP poll、JSON/base64 和小批量带来的额外延迟。
- [ ] 网络切换、休眠唤醒、NAT 变化后，恢复路径要稳定，不应反复重建或长时间卡在不可用状态。
- [ ] 优化过程中保留三层 LAN 能力，不把 UDPTunnelLAN 退化成单纯端口转发产品。

## P0：先拉近核心数据面差距

### P0-1 为 LAN packet 建立独立高性能传输参数

当前问题：

- UDPTunnelLAN 使用 `store.ProfileLANPacket`。
- `internal/tunnel/profileConfig` 只有 `bulk` 使用大 KCP window 和大 UDP socket buffer。
- `lan-packet` 目前落到默认 KCP window `1024`，没有 `16MB` UDP socket buffer。
- Agent bulk 使用更大的 KCP window、UDP socket buffer、smux buffer 和 TCP socket buffer，吞吐能力明显更强。

任务：

- [x] 为 `lan-packet` 增加独立 profile config，不影响 `interactive` 和 `bulk`。
- [x] 将 LAN KCP window 提升到接近 bulk 的级别，作为第一轮吞吐优化。
- [x] 为 LAN KCP 路径设置较大的 UDP read/write buffer。
- [x] 增加测试覆盖 `lan-packet` profile 使用独立参数。
- [ ] 压测 P2P KCP 路径吞吐，记录优化前后差异。

### P0-2 优先使用 UDP datagram fast path 承载原始 IP 包

当前问题：

- 原始 IP 包如果走 `writeLANFrame(peer.kcp, payload)`，会进入单条可靠有序 KCP 字节流。
- 内层 TCP 已经有可靠性、重传和拥塞控制，外层 KCP 再可靠有序传输会产生 head-of-line blocking 和重复重传。
- 丢一个外层 KCP 包，可能阻塞所有虚拟 LAN 流量。

任务：

- [x] 产品决策：允许 LAN 原始 IP 包默认绕开 KCP，首选加密 UDP datagram；KCP 仅作为控制、兼容或临时兜底。
- [x] 明确 UDPTunnelLAN 原始 IP 包的首选路径为加密 UDP datagram。
- [x] 梳理 `datagramReady` 的建立条件，减少已经 P2P 可达但仍走 KCP 的时间窗口。
- [x] P2P punch / ack 成功后尽快切换 raw IP 包到 datagram path。
- [x] KCP 仅作为临时兜底、控制确认或未支持 datagram 时的兼容路径。
- [x] 增加测试覆盖 datagram ready 后 raw IP 包不再落入 KCP stream。
- [x] 增加日志或状态字段，区分 `p2p_datagram`、`p2p_kcp`、`relay_http`，便于现场判断真实路径。

### P0-3 降低 KCP stream 路径的阻塞影响

当前问题：

- 目前 KCP stream 是单连接、单可靠有序流。
- 如果多个虚拟 LAN 连接共享这条流，一个流量的丢包会阻塞其他流量。

任务：

- [ ] 在 datagram 未就绪时，减少单条 KCP stream 对全部流量的影响。
- [ ] 评估按 peer 多 KCP session、按流分类或分优先级队列的可行性。
- [x] 对 ICMP、TCP SYN、交互小包给予更高发送优先级。
- [x] 避免大包或持续吞吐流量阻塞交互流量。
- [ ] 增加测试或压测覆盖“文件传输同时 ping/RDP”的延迟表现。

## P1：重做 relay 性能路径

### P1-1 将 LAN relay 从 HTTP JSON/base64 poll 升级为低延迟数据面

当前问题：

- 当前 LAN relay 使用 HTTP POST/poll、JSON、base64 和服务端内存队列。
- 服务端空队列 poll 每 `100ms` 检查一次。
- 客户端每批默认 `16` 个包。
- base64 会放大 payload，JSON 编解码增加 CPU 和延迟。
- Agent 的 relay 仍然走 UDP/KCP 数据面，性能模型更轻。

任务：

- [x] 产品决策：LAN UDP relay 实现完全独立通道，不复用老 Agent UDP relay 服务端通道。
- [x] 安全决策：LAN UDP relay 继续保持端到端加密，服务端只转发密文，不解密原始 IP 包。
- [x] 设计 LAN 专用 UDP relay 数据面，避免 HTTP JSON/base64 承载高频 IP 包。
- [x] 服务端支持 LAN relay 二进制帧转发。
- [x] 客户端 P2P 不可用时优先进入 LAN UDP relay，而不是 HTTP poll relay。
- [x] LAN UDP relay 只识别最小转发头和目标设备，不读取明文 IP payload。
- [x] 保留 HTTP relay 作为最保守兜底或诊断路径。
- [ ] 增加 relay 路径压测，分别记录 HTTP relay 和 UDP relay 的 RTT、吞吐、丢包恢复。

### P1-3 增加 TCP fast path

当前问题：

- UDPTunnelLAN 保留三层 LAN 能力，但 RDP、SMB、文件传输等常见业务主要是 TCP。
- 原始 IP 包走通用包隧道时，TCP over KCP 或 TCP over relay 容易出现重复拥塞控制和 head-of-line blocking。
- Agent 的 TCP 流代理模型在这些场景下更接近业务本身，吞吐和稳定性更好。

任务：

- [x] 产品决策：为 UDPTunnelLAN 增加 TCP fast path，同时保留三层 LAN 能力。
- [ ] 识别可进入 fast path 的 TCP 流量，例如 RDP、SMB、文件传输或后台配置的端口集合。
- [ ] fast path 采用类似 Agent 的 TCP stream 转发模型，但实现独立于 `cmd/client`。
- [ ] fast path 与原始 IP 包路径共存，不能破坏 ICMP、UDP 和普通三层互通。
- [ ] 管理后台增加 TCP fast path 策略配置和状态展示。
- [ ] 增加测试覆盖 fast path 开启、关闭、fallback 到通用三层路径。

### P1-2 在 HTTP relay 保留期间做临时优化

当前问题：

- UDP relay 完成前，HTTP relay 仍是很多 NAT 场景下的可用性兜底。
- 当前轮询 tick、批量大小、JSON/base64 设计会明显放大延迟。

任务：

- [x] 增大 poll 批量上限，降低高包率下的请求次数。
- [x] 降低服务端空队列唤醒延迟，减少 `100ms` ticker 对交互延迟的影响。
- [x] 复用 HTTP client，避免每次 `postLANJSON` 新建 client。
- [x] 评估 gzip 是否对小包有负收益，避免盲目启用压缩。
- [x] 增加 relay queue 溢出、延迟、批量大小的指标和日志。

## P2：优化连接建立和路径选择

### P2-1 引入 NAT 分类和更准确的 relay 决策

当前问题：

- UDPTunnelLAN 初始 P2P 尝试窗口为 `30s`。
- 对称 NAT、企业网、运营商 CGNAT 下，等待 30 秒往往只是延迟进入 relay。
- Agent 已有 NAT 结果和 relay-first 判断，坏 NAT 下更快进入可用路径。

任务：

- [x] 产品决策：管理后台默认路径策略为优先 P2P。
- [x] 为 UDPTunnelLAN 引入 NAT 探测或复用服务端可提供的 NAT 判断结果。
- [x] 对后台配置为优先 relay 或仅 relay 的场景直接进入 relay 数据面，同时保留 P2P 后台注册/打洞。
- [x] 保留用户确认的“初始 P2P 尝试 30 秒”语义，但允许坏 NAT 快速启用 relay 承接流量。
- [x] 后台状态展示 NAT 类型、当前路径和 fallback 原因。
- [x] 管理后台提供路径策略配置，第一阶段默认 `优先 P2P`，后续可扩展自动、优先 relay、仅 relay。
- [x] 增加测试覆盖 relay-first、P2P 后台打通后切换回 P2P。

### P2-2 改善早期包处理和队列策略

当前问题：

- 虚拟网卡启动后，用户可能立即 ping、RDP 或访问共享。
- 隧道未就绪时，早期 SYN/ICMP 容易排队过短、过期或走低效路径。

任务：

- [x] 区分早期关键包和普通数据包，优先保留 TCP SYN、ICMP echo、DNS 等小包。
- [x] 调整 pending queue 的容量、TTL 和淘汰策略。
- [x] 增加 pending queue 指标：当前长度、丢弃数、过期数、按协议统计。
- [x] 路径就绪后快速 replay pending 包，减少用户第一次连接失败概率。

### P2-3 优化 Wintun 读写和批处理

当前问题：

- UDPTunnelLAN 每个原始 IP 包都经过 Wintun read、路由、封装、发送、接收、Wintun write。
- 高包率下系统调用、内存拷贝和调度开销会放大。

任务：

- [x] 检查 Wintun session ring size、读等待和写入错误处理是否适合高吞吐。
- [x] 减少不必要的 payload copy，尤其是路由和 relay 封装阶段。
- [x] 评估 outbound channel `256` 容量是否过小。
- [x] 增加 Wintun read/write 错误、队列满、包大小分布的诊断日志或指标。
- [ ] 压测不同 MTU、MSS 下的吞吐和丢包表现。

### P2-4 支持按网络环境配置 MTU/MSS

当前问题：

- UDPTunnelLAN 当前默认 MTU/MSS 较保守。
- 不同网络环境、relay 路径、运营商链路和企业网设备对分片、MTU、MSS 的容忍度不同。

任务：

- [x] 产品决策：需要为不同网络环境提供可配置 MTU/MSS。
- [x] 管理后台增加 UDPTunnelLAN 网络级 MTU/MSS 配置。
- [x] bootstrap 下发网络级 MTU/MSS。
- [x] 客户端按服务端配置设置 Wintun MTU 和 TCP MSS clamp。
- [x] 增加配置校验，避免 MTU/MSS 设置到明显不可用范围。
- [ ] 增加不同 MTU/MSS 组合的连通性和吞吐测试。

### P2-5 按流量类型选择低延迟或高吞吐

当前问题：

- 交互通讯更需要低延迟。
- 文件传输更需要吞吐。
- 单一策略无法同时兼顾 RDP/实时交互和大文件传输。

任务：

- [x] 产品决策：即时交互通讯低延迟优先，文件传输吞吐优先。
- [x] 定义交互流量和吞吐流量的识别规则。
- [x] 对交互小包、TCP SYN/ACK、RDP 等低延迟优先处理。
- [ ] 对 SMB、文件传输、持续大流量启用吞吐优先参数或 TCP fast path。
- [x] 管理后台展示当前策略和命中的流量类别。

## P3：接近 Agent 的用户体验和诊断能力

### P3-1 增加路径可观测性

当前问题：

- 现在用户只感知“慢”或“不可用”，很难知道走的是 P2P datagram、P2P KCP 还是 HTTP relay。

任务：

- [x] 客户端日志明确输出每个 peer 当前路径：`p2p_datagram`、`p2p_kcp`、`relay_udp`、`relay_http`。
- [x] 管理后台展示每个 LAN peer 的当前路径、RTT、估算吞吐、最近切换原因。
- [x] 记录路径切换时间线，便于判断是否频繁抖动。
- [x] 增加一键导出 UDPTunnelLAN 诊断信息。

### P3-2 建立性能基线和回归测试

当前问题：

- 当前缺少固定性能基线，很难判断每轮优化是否接近 Agent。

任务：

- [ ] 制定本地同网、跨 NAT、HTTP relay、未来 UDP relay 四类测试场景。
- [ ] 使用 ping、iperf3、RDP 体感、文件复制分别记录指标。
- [ ] 同场景对照 Agent interactive 和 Agent bulk。
- [ ] 将关键压测命令和结果写入文档。
- [ ] 每轮性能优化后更新基线数据。
