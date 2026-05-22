import { DeleteOutlined, EyeOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, message, Popconfirm, Space, Switch, Table, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useState } from 'react'
import { deleteDevice, listDevices, setDeviceEnabled } from '../api/devices'
import { listRules } from '../api/rules'
import { DeviceDetailDrawer } from '../components/DeviceDetailDrawer'
import { StatusTag } from '../components/StatusTag'
import type { Device } from '../types/api'

export function DevicesPage() {
  const queryClient = useQueryClient()
  const [selectedDevice, setSelectedDevice] = useState<Device | null>(null)
  const devices = useQuery({
    queryKey: ['devices'],
    queryFn: listDevices,
    refetchInterval: 15000,
  })
  const rules = useQuery({
    queryKey: ['rules'],
    queryFn: listRules,
  })
  const refreshDevices = () => queryClient.invalidateQueries({ queryKey: ['devices'] })
  const enabledMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => setDeviceEnabled(id, enabled),
    onSuccess: () => refreshDevices(),
    onError: (err) => message.error(err instanceof Error ? err.message : '状态更新失败'),
  })
  const deleteMutation = useMutation({
    mutationFn: deleteDevice,
    onSuccess: () => {
      message.success('设备已删除')
      refreshDevices()
      queryClient.invalidateQueries({ queryKey: ['rules'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>设备</Typography.Title>
          <Typography.Text type="secondary">查看 agent 在线状态、地址和健康摘要。</Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={() => devices.refetch()} loading={devices.isFetching}>
          刷新
        </Button>
      </div>
      <Card>
        <Table
          rowKey="id"
          loading={devices.isLoading}
          dataSource={devices.data || []}
          columns={[
            { title: '状态', dataIndex: 'online', width: 96, render: (_, r) => <StatusTag online={r.online} enabled={r.enabled} /> },
            { title: '设备名', dataIndex: 'name', render: (v, r) => <Space direction="vertical" size={0}><Typography.Text strong>{v || r.id}</Typography.Text><Typography.Text type="secondary" copyable>{r.id}</Typography.Text></Space> },
            { title: '公网地址', dataIndex: 'addr', render: (v) => v ? <Typography.Text copyable>{v}</Typography.Text> : '-' },
            { title: 'UPnP 地址', dataIndex: 'upnp_addr', render: (v) => v ? <Typography.Text copyable>{v}</Typography.Text> : '-' },
            { title: '健康摘要', dataIndex: 'health_summary', render: (v) => v || '-' },
            { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
            { title: '最后心跳', dataIndex: 'last_seen', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
            { title: '启用', dataIndex: 'enabled', width: 88, render: (v, r) => <Switch checked={v} loading={enabledMutation.isPending} onChange={(checked) => enabledMutation.mutate({ id: r.id, enabled: checked })} /> },
            {
              title: '操作',
              width: 140,
              render: (_, r) => (
                <Space>
                  <Button size="small" icon={<EyeOutlined />} onClick={() => setSelectedDevice(r)} />
                  <Popconfirm title="删除设备" description="仅无启用规则引用时可删除，确认继续？" onConfirm={() => deleteMutation.mutate(r.id)}>
                    <Button size="small" danger icon={<DeleteOutlined />} />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
          scroll={{ x: 1300 }}
        />
      </Card>
      <DeviceDetailDrawer
        open={Boolean(selectedDevice)}
        device={selectedDevice}
        rules={rules.data || []}
        onClose={() => setSelectedDevice(null)}
      />
    </div>
  )
}
