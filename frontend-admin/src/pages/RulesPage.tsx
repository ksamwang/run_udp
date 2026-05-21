import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Space, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { listRules } from '../api/rules'
import { StatusTag } from '../components/StatusTag'

export function RulesPage() {
  const rules = useQuery({
    queryKey: ['rules'],
    queryFn: listRules,
    refetchInterval: 15000,
  })

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>转发规则</Typography.Title>
          <Typography.Text type="secondary">查看入口设备、本地端口、出口目标和当前运行状态。</Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={() => rules.refetch()} loading={rules.isFetching}>
          刷新
        </Button>
      </div>
      <Card>
        <Table
          rowKey="id"
          loading={rules.isLoading}
          dataSource={rules.data || []}
          columns={[
            { title: '状态', dataIndex: 'runtime_state', width: 110, render: (v, r) => <StatusTag state={r.enabled ? v : 'disabled'} /> },
            { title: '规则名', dataIndex: 'name', render: (v, r) => <Space direction="vertical" size={0}><Typography.Text strong>{v || `#${r.id}`}</Typography.Text><Typography.Text type="secondary">#{r.id}</Typography.Text></Space> },
            { title: '入口设备', dataIndex: 'source_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: '本地端口', dataIndex: 'local_port' },
            { title: '出口设备', dataIndex: 'target_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: '目标地址', render: (_, r) => <Typography.Text copyable>{`${r.target_host}:${r.target_port}`}</Typography.Text> },
            { title: 'Profile', dataIndex: 'profile', render: (v) => <Tag color={v === 'bulk' ? 'purple' : 'cyan'}>{v || 'interactive'}</Tag> },
            { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
          ]}
          scroll={{ x: 1100 }}
        />
      </Card>
    </div>
  )
}
