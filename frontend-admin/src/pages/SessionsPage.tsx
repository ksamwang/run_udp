import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { listSessions } from '../api/sessions'

export function SessionsPage() {
  const sessions = useQuery({
    queryKey: ['sessions'],
    queryFn: listSessions,
    refetchInterval: 15000,
  })

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>会话</Typography.Title>
          <Typography.Text type="secondary">查看最近隧道会话、路径和 Relay 流量。</Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={() => sessions.refetch()} loading={sessions.isFetching}>刷新</Button>
      </div>
      <Card>
        <Table
          rowKey="id"
          loading={sessions.isLoading}
          dataSource={sessions.data || []}
          columns={[
            { title: 'ID', dataIndex: 'id', width: 80 },
            { title: 'Source', dataIndex: 'source_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: 'Target', dataIndex: 'target_id', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: 'Profile', dataIndex: 'profile', render: (v) => <Tag color={v === 'bulk' ? 'purple' : 'cyan'}>{v || 'interactive'}</Tag> },
            { title: 'Path', dataIndex: 'path', render: (v) => <Tag color={v === 'relay' ? 'blue' : v === 'p2p' ? 'green' : undefined}>{v || 'pending'}</Tag> },
            { title: 'Relay Bytes', dataIndex: 'relay_bytes' },
            { title: 'Started', dataIndex: 'started_at', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
            { title: 'Last Seen', dataIndex: 'last_seen', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
            { title: 'Ended', dataIndex: 'ended_at', render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : <Tag color="green">活跃</Tag> },
          ]}
          scroll={{ x: 1100 }}
        />
      </Card>
    </div>
  )
}
