import { Descriptions, Drawer, Empty, Table, Tabs, Typography } from 'antd'
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
                  <Descriptions.Item label="状态"><StatusTag online={device.online} enabled={device.enabled} /></Descriptions.Item>
                  <Descriptions.Item label="设备名">{device.name || '-'}</Descriptions.Item>
                  <Descriptions.Item label="设备 ID"><Typography.Text copyable>{device.id}</Typography.Text></Descriptions.Item>
                  <Descriptions.Item label="公网地址">{device.addr ? <Typography.Text copyable>{device.addr}</Typography.Text> : '-'}</Descriptions.Item>
                  <Descriptions.Item label="端口映射地址">{device.upnp_addr ? <Typography.Text copyable>{device.upnp_addr}</Typography.Text> : '-'}</Descriptions.Item>
                  <Descriptions.Item label="健康摘要">{device.health_summary || '-'}</Descriptions.Item>
                  <Descriptions.Item label="最近错误">{device.last_error || '-'}</Descriptions.Item>
                  <Descriptions.Item label="最后心跳">{device.last_seen ? dayjs(device.last_seen).format('YYYY-MM-DD HH:mm:ss') : '-'}</Descriptions.Item>
                </Descriptions>
              ),
            },
            {
              key: 'rules',
              label: `关联规则 ${related.length}`,
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
