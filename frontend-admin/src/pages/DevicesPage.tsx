import { DeleteOutlined, EyeOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, message, Popconfirm, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useEffect, useState } from 'react'
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
    onMutate: async ({ id, enabled }) => {
      await queryClient.cancelQueries({ queryKey: ['devices'] })
      const previous = queryClient.getQueryData<Device[]>(['devices'])
      queryClient.setQueryData<Device[]>(['devices'], (old) =>
        old?.map((item) => item.id === id ? { ...item, enabled } : item) || old,
      )
      return { previous }
    },
    onSuccess: () => refreshDevices(),
    onError: (err, _vars, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['devices'], context.previous)
      }
      message.error(err instanceof Error ? err.message : '状态更新失败，已恢复原状态')
    },
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

  useEffect(() => {
    if (!selectedDevice || !devices.data || devices.isFetching) {
      return
    }
    const latest = devices.data.find((device) => device.id === selectedDevice.id)
    setSelectedDevice(latest || null)
  }, [devices.data, devices.isFetching, selectedDevice])

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>设备</Typography.Title>
          <Typography.Text type="secondary">统一设备资产总览；Agent 转发和 UDPTunnelLAN 虚拟局域网状态将按产品分开展示。</Typography.Text>
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
            {
              title: '产品能力',
              dataIndex: 'product_capabilities',
              width: 180,
              render: (_, r) => {
                const caps = r.product_capabilities || []
                return caps.length ? <Space size={4} wrap>{caps.map((cap) => <Tag key={cap}>{cap}</Tag>)}</Space> : <Typography.Text type="secondary">未发现产品</Typography.Text>
              },
            },
            { title: 'Agent', dataIndex: 'agent_online', width: 110, render: (_, r) => r.agent_last_source ? <StatusTag online={r.agent_online} enabled={r.enabled} /> : <Tag>未发现</Tag> },
            { title: 'UDPTunnelLAN', dataIndex: 'lan_online', width: 140, render: (_, r) => r.lan_last_source ? <StatusTag online={r.lan_online} enabled={r.enabled} /> : <Tag>未发现</Tag> },
            { title: '设备名', dataIndex: 'name', render: (v, r) => <Space direction="vertical" size={0}><Typography.Text strong>{v || r.id}</Typography.Text><Typography.Text type="secondary" copyable>{r.id}</Typography.Text></Space> },
            { title: '最近来源', render: (_, r) => <Space direction="vertical" size={0}><Typography.Text>Agent: {r.agent_last_source || '-'}</Typography.Text><Typography.Text>LAN: {r.lan_last_source || '-'}</Typography.Text></Space> },
            { title: '最近 Agent 上报', dataIndex: 'last_agent_seen', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
            { title: '最近 LAN 上报', dataIndex: 'last_lan_seen', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
            { title: '最近错误', render: (_, r) => r.last_error || r.lan_last_error || '-' },
            { title: '启用', dataIndex: 'enabled', width: 88, render: (v, r) => <Switch checked={v} loading={enabledMutation.isPending} onChange={(checked) => enabledMutation.mutate({ id: r.id, enabled: checked })} /> },
            {
              title: '操作',
              width: 140,
              render: (_, r) => (
                <Space>
                  <Tooltip title="查看详情">
                    <Button size="small" icon={<EyeOutlined />} onClick={() => setSelectedDevice(r)} />
                  </Tooltip>
                  <Popconfirm title="删除设备" description="仅无启用规则引用时可删除，确认继续？" onConfirm={() => deleteMutation.mutate(r.id)}>
                    <Tooltip title="删除设备">
                      <Button size="small" danger icon={<DeleteOutlined />} loading={deleteMutation.isPending} />
                    </Tooltip>
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
