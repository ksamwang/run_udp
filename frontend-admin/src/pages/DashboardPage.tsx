import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Col, Row, Statistic, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { getHealth } from '../api/metrics'
import { listTunnelStates } from '../api/tunnels'
import { StatusTag } from '../components/StatusTag'

export function DashboardPage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: getHealth,
    refetchInterval: 15000,
  })
  const tunnels = useQuery({
    queryKey: ['tunnel-states'],
    queryFn: listTunnelStates,
    refetchInterval: 15000,
  })
  const metrics = health.data?.metrics
  const tunnelRows = tunnels.data || []
  const abnormalCount = tunnelRows.filter((t) => !['p2p', 'relay', 'disabled'].includes((t.state || '').toLowerCase())).length

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>总览</Typography.Title>
          <Typography.Text type="secondary">
            {health.data?.server_time ? `服务器时间 ${dayjs(health.data.server_time).format('YYYY-MM-DD HH:mm:ss')}` : '等待服务器数据'}
          </Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={() => health.refetch()} loading={health.isFetching}>
          刷新
        </Button>
      </div>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="在线设备" value={metrics?.online_devices ?? 0} suffix={`/ ${metrics?.devices ?? 0}`} /></Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="转发规则" value={metrics?.forward_rules ?? 0} /></Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="活跃会话" value={metrics?.active_sessions ?? 0} /></Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="中继流量" value={metrics?.relay_bytes ?? 0} /></Card>
        </Col>
      </Row>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="隧道状态" value={tunnelRows.length} /></Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card><Statistic title="异常隧道" value={abnormalCount} valueStyle={{ color: abnormalCount > 0 ? '#cf1322' : undefined }} /></Card>
        </Col>
      </Row>
      <Card title="隧道健康">
        <Table
          size="middle"
          rowKey={(r) => `${r.device_id}-${r.peer_id}-${r.profile}`}
          loading={tunnels.isLoading}
          pagination={{ pageSize: 8 }}
          dataSource={tunnelRows}
          columns={[
            { title: '设备', dataIndex: 'device_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: '对端', dataIndex: 'peer_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: '连接模式', dataIndex: 'profile', render: (v) => <Tag color={v === 'bulk' ? 'purple' : 'cyan'}>{formatProfile(v)}</Tag> },
            { title: '状态', dataIndex: 'state', render: (v) => <StatusTag state={v} /> },
            { title: '路径', dataIndex: 'via', render: (v) => v || '-' },
            { title: 'RTT', dataIndex: 'rtt_ms', render: (v) => v ? `${v} ms` : '-' },
            { title: 'NAT', dataIndex: 'nat_type', render: (v) => v || '-' },
            { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
            { title: '更新时间', dataIndex: 'updated_at', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
          ]}
          scroll={{ x: 1100 }}
        />
      </Card>
    </div>
  )
}

function formatProfile(profile?: string) {
  return profile === 'bulk' ? '吞吐优先' : '交互优先'
}
