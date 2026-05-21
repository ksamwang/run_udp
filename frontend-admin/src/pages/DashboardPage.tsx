import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Col, Row, Statistic, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { getHealth } from '../api/metrics'

export function DashboardPage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: getHealth,
    refetchInterval: 15000,
  })
  const metrics = health.data?.metrics

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
          <Card><Statistic title="Relay 流量" value={metrics?.relay_bytes ?? 0} /></Card>
        </Col>
      </Row>
      <Card title="隧道健康">
        <Table
          size="middle"
          pagination={false}
          dataSource={[]}
          columns={[
            { title: '入口设备', dataIndex: 'source' },
            { title: '出口设备', dataIndex: 'target' },
            { title: '状态', dataIndex: 'state', render: () => <Tag>暂无数据</Tag> },
            { title: '最近错误', dataIndex: 'error' },
          ]}
          locale={{ emptyText: '下一阶段接入隧道状态列表' }}
        />
      </Card>
    </div>
  )
}
