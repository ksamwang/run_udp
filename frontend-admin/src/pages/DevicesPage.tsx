import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Space, Table, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { listDevices } from '../api/devices'
import { StatusTag } from '../components/StatusTag'

export function DevicesPage() {
  const devices = useQuery({
    queryKey: ['devices'],
    queryFn: listDevices,
    refetchInterval: 15000,
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
          ]}
          scroll={{ x: 1100 }}
        />
      </Card>
    </div>
  )
}
