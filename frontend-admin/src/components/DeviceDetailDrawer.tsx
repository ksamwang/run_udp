import { Descriptions, Drawer, Empty, Table, Tabs, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import type { Device, ForwardRule } from '../types/api'
import { StatusTag } from './StatusTag'

type DeviceDetailDrawerProps = {
  open: boolean
  device?: Device | null
  rules: ForwardRule[]
  onClose: () => void
}

export function DeviceDetailDrawer({ open, device, rules, onClose }: DeviceDetailDrawerProps) {
  const related = device ? rules.filter((r) => r.source_id === device.id || r.target_id === device.id) : []
  return (
    <Drawer title="设备详情" open={open} onClose={onClose} width={640}>
      {!device ? <Empty /> : (
        <Tabs
          items={[
            {
              key: 'overview',
              label: '概览',
              children: (
                <Descriptions column={1} bordered size="small">
                  <Descriptions.Item label="设备名">{device.name || '-'}</Descriptions.Item>
                  <Descriptions.Item label="设备 ID"><Typography.Text copyable>{device.id}</Typography.Text></Descriptions.Item>
                  <Descriptions.Item label="启用状态">{device.enabled ? <Tag color="green">启用</Tag> : <Tag color="red">停用</Tag>}</Descriptions.Item>
                  <Descriptions.Item label="产品能力">{device.product_capabilities?.length ? device.product_capabilities.join(' + ') : '未发现产品'}</Descriptions.Item>
                  <Descriptions.Item label="Agent 在线状态">{device.agent_last_source ? <StatusTag online={device.agent_online} enabled={device.enabled} /> : '未发现 Agent'}</Descriptions.Item>
                  <Descriptions.Item label="LAN 在线状态">{device.lan_last_source ? <StatusTag online={device.lan_online} enabled={device.enabled} /> : '未发现 UDPTunnelLAN'}</Descriptions.Item>
                  <Descriptions.Item label="最近 Agent 上报">{device.last_agent_seen ? dayjs(device.last_agent_seen).format('YYYY-MM-DD HH:mm:ss') : '-'}</Descriptions.Item>
                  <Descriptions.Item label="最近 LAN 上报">{device.last_lan_seen ? dayjs(device.last_lan_seen).format('YYYY-MM-DD HH:mm:ss') : '-'}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'agent',
              label: 'Agent',
              children: (
                <Descriptions column={1} bordered size="small">
                  <Descriptions.Item label="状态">{device.agent_last_source ? <StatusTag online={device.agent_online} enabled={device.enabled} /> : '未发现 Agent'}</Descriptions.Item>
                  <Descriptions.Item label="最近来源">{device.agent_last_source || '-'}</Descriptions.Item>
                  <Descriptions.Item label="公网地址">{device.addr ? <Typography.Text copyable>{device.addr}</Typography.Text> : '-'}</Descriptions.Item>
                  <Descriptions.Item label="UPnP 地址">{device.upnp_addr ? <Typography.Text copyable>{device.upnp_addr}</Typography.Text> : '-'}</Descriptions.Item>
                  <Descriptions.Item label="当前 want / peer">{device.want || '-'}</Descriptions.Item>
                  <Descriptions.Item label="关联转发规则数量">{related.length}</Descriptions.Item>
                  <Descriptions.Item label="Agent 隧道状态摘要">{device.health_summary || '-'}</Descriptions.Item>
                  <Descriptions.Item label="Agent 最近错误">{device.last_error || '-'}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'lan',
              label: 'UDPTunnelLAN',
              children: (
                <Descriptions column={1} bordered size="small">
                  <Descriptions.Item label="状态">{device.lan_last_source ? <StatusTag online={device.lan_online} enabled={device.enabled} /> : '未发现 UDPTunnelLAN'}</Descriptions.Item>
                  <Descriptions.Item label="最近来源">{device.lan_last_source || '-'}</Descriptions.Item>
                  <Descriptions.Item label="虚拟 IP">{device.lan_virtual_ip || '-'}</Descriptions.Item>
                  <Descriptions.Item label="虚拟网络">{device.lan_network_id || '-'}</Descriptions.Item>
                  <Descriptions.Item label="虚拟网卡状态">{device.lan_adapter_state || '-'}</Descriptions.Item>
                  <Descriptions.Item label="选择网段">{device.lan_selected_cidr || '-'}</Descriptions.Item>
                  <Descriptions.Item label="路由冲突">{device.lan_route_conflict || '-'}</Descriptions.Item>
                  <Descriptions.Item label="路径摘要">{device.lan_path_summary || '-'}</Descriptions.Item>
                  <Descriptions.Item label="活跃会话数">{device.lan_active_sessions ?? 0}</Descriptions.Item>
                  <Descriptions.Item label="热路径数">{device.lan_hot_paths ?? 0}</Descriptions.Item>
                  <Descriptions.Item label="Socket rotation">{device.lan_socket_rotations ?? 0}</Descriptions.Item>
                  <Descriptions.Item label="最近 rotation 原因">{device.lan_last_rotation_reason || '-'}</Descriptions.Item>
                  <Descriptions.Item label="LAN 最近错误">{device.lan_last_error || '-'}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'rules',
              label: `Agent 规则 ${related.length}`,
              children: (
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={related}
                  columns={[
                    { title: '规则', dataIndex: 'name', render: (v, r) => v || `#${r.id}` },
                    { title: '方向', render: (_, r) => r.source_id === device.id ? '入口' : '出口' },
                    { title: '本地端口', dataIndex: 'local_port' },
                    { title: '目标', render: (_, r) => `${r.target_host}:${r.target_port}` },
                    { title: '状态', dataIndex: 'runtime_state', render: (v, r) => <StatusTag state={r.enabled ? v : 'disabled'} /> },
                  ]}
                />
              ),
            },
          ]}
        />
      )}
    </Drawer>
  )
}
