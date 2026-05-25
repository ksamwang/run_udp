import { DeleteOutlined, EditOutlined, EyeOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Descriptions, Drawer, message, Popconfirm, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useState } from 'react'
import { listDevices } from '../api/devices'
import { createRule, deleteRule, listRules, setRuleEnabled, updateRule } from '../api/rules'
import { listSessions } from '../api/sessions'
import { listTunnelStates } from '../api/tunnels'
import { RuleFormDrawer } from '../components/RuleFormDrawer'
import { StatusTag } from '../components/StatusTag'
import type { ForwardRule, ForwardRulePayload, Session, TunnelState } from '../types/api'

export function RulesPage() {
  const queryClient = useQueryClient()
  const [editingRule, setEditingRule] = useState<ForwardRule | null>(null)
  const [detailRule, setDetailRule] = useState<ForwardRule | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const rules = useQuery({
    queryKey: ['rules'],
    queryFn: listRules,
    refetchInterval: 15000,
  })
  const devices = useQuery({
    queryKey: ['devices'],
    queryFn: listDevices,
  })
  const tunnels = useQuery({
    queryKey: ['tunnel-states'],
    queryFn: listTunnelStates,
    refetchInterval: 15000,
  })
  const sessions = useQuery({
    queryKey: ['sessions'],
    queryFn: listSessions,
    refetchInterval: 15000,
  })
  const refreshRules = () => queryClient.invalidateQueries({ queryKey: ['rules'] })
  const saveMutation = useMutation<unknown, Error, ForwardRulePayload>({
    mutationFn: (payload: ForwardRulePayload) => editingRule ? updateRule(editingRule.id, payload) : createRule(payload),
    onSuccess: () => {
      message.success('规则已保存')
      setDrawerOpen(false)
      setEditingRule(null)
      refreshRules()
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const deleteMutation = useMutation({
    mutationFn: deleteRule,
    onSuccess: () => {
      message.success('规则已删除')
      refreshRules()
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '删除失败'),
  })
  const enabledMutation = useMutation({
    mutationFn: ({ rule, enabled }: { rule: ForwardRule; enabled: boolean }) => setRuleEnabled(rule, enabled),
    onMutate: async ({ rule, enabled }) => {
      await queryClient.cancelQueries({ queryKey: ['rules'] })
      const previous = queryClient.getQueryData<ForwardRule[]>(['rules'])
      queryClient.setQueryData<ForwardRule[]>(['rules'], (old) =>
        old?.map((item) => item.id === rule.id ? { ...item, enabled } : item) || old,
      )
      return { previous }
    },
    onSuccess: () => refreshRules(),
    onError: (err, _vars, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['rules'], context.previous)
      }
      message.error(err instanceof Error ? err.message : '状态更新失败，已恢复原状态')
    },
  })

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>Agent 转发规则</Typography.Title>
          <Typography.Text type="secondary">仅适用于老 Agent 客户端；UDPTunnelLAN 设备请在虚拟局域网页管理。</Typography.Text>
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => rules.refetch()} loading={rules.isFetching}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditingRule(null); setDrawerOpen(true) }}>新增 Agent 规则</Button>
        </Space>
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
            { title: '连接模式', dataIndex: 'profile', render: (v) => <Tag color={v === 'bulk' ? 'purple' : 'cyan'}>{formatProfile(v)}</Tag> },
            { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
            { title: '启用', dataIndex: 'enabled', width: 88, render: (v, r) => <Switch checked={v} loading={enabledMutation.isPending} onChange={(checked) => enabledMutation.mutate({ rule: r, enabled: checked })} /> },
            {
              title: '操作',
              width: 140,
              render: (_, r) => (
                <Space>
                  <Tooltip title="查看诊断">
                    <Button size="small" icon={<EyeOutlined />} onClick={() => setDetailRule(r)} />
                  </Tooltip>
                  <Tooltip title="编辑规则">
                    <Button size="small" icon={<EditOutlined />} onClick={() => { setEditingRule(r); setDrawerOpen(true) }} />
                  </Tooltip>
                  <Popconfirm title="删除 Agent 规则" description="确认删除这条 Agent 转发规则？" onConfirm={() => deleteMutation.mutate(r.id)}>
                    <Tooltip title="删除规则">
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
      <RuleFormDrawer
        open={drawerOpen}
        devices={devices.data || []}
        rule={editingRule}
        submitting={saveMutation.isPending}
        onClose={() => { setDrawerOpen(false); setEditingRule(null) }}
        onSubmit={(payload) => saveMutation.mutate(payload)}
      />
      <RuleDetailDrawer
        rule={detailRule}
        tunnels={tunnels.data || []}
        sessions={sessions.data || []}
        onClose={() => setDetailRule(null)}
      />
    </div>
  )
}

function formatProfile(profile?: string) {
  return profile === 'bulk' ? '吞吐优先' : '交互优先'
}

function RuleDetailDrawer({ rule, tunnels, sessions, onClose }: { rule: ForwardRule | null; tunnels: TunnelState[]; sessions: Session[]; onClose: () => void }) {
  const relatedTunnels = rule ? tunnels.filter((t) =>
    t.profile === rule.profile && (
      (t.device_id === rule.source_id && t.peer_id === rule.target_id) ||
      (t.device_id === rule.target_id && t.peer_id === rule.source_id)
    ),
  ) : []
  const relatedSessions = rule ? sessions.filter((s) =>
    s.profile === rule.profile && s.source_id === rule.source_id && s.target_id === rule.target_id,
  ) : []
  const primary = relatedTunnels[0]
  return (
    <Drawer title={rule ? `规则诊断：${rule.name || `#${rule.id}`}` : '规则诊断'} open={Boolean(rule)} onClose={onClose} width={760}>
      {rule ? (
        <Space direction="vertical" size="large" className="full-width">
          <Descriptions bordered column={2} size="small">
            <Descriptions.Item label="入口设备">{rule.source_id}</Descriptions.Item>
            <Descriptions.Item label="出口设备">{rule.target_id}</Descriptions.Item>
            <Descriptions.Item label="连接模式">{formatProfile(rule.profile)}</Descriptions.Item>
            <Descriptions.Item label="本地端口">{rule.local_port}</Descriptions.Item>
            <Descriptions.Item label="目标地址">{`${rule.target_host}:${rule.target_port}`}</Descriptions.Item>
            <Descriptions.Item label="运行状态"><StatusTag state={rule.enabled ? rule.runtime_state : 'disabled'} /></Descriptions.Item>
            <Descriptions.Item label="NAT">{primary?.nat_type || '-'}</Descriptions.Item>
            <Descriptions.Item label="路径">{primary?.via || primary?.state || '-'}</Descriptions.Item>
            <Descriptions.Item label="RTT">{primary?.rtt_ms ? `${primary.rtt_ms} ms` : '-'}</Descriptions.Item>
            <Descriptions.Item label="尝试次数">{primary?.attempt ?? rule.attempt ?? '-'}</Descriptions.Item>
            <Descriptions.Item label="下次重试">{formatTime(primary?.next_retry_at || rule.next_retry_at)}</Descriptions.Item>
            <Descriptions.Item label="状态更新时间">{formatTime(primary?.updated_at || rule.last_updated_at)}</Descriptions.Item>
            <Descriptions.Item label="最近错误" span={2}>{primary?.last_error || rule.last_error || '-'}</Descriptions.Item>
          </Descriptions>
          <Card title="关联隧道" size="small">
            <Table
              size="small"
              rowKey={(row) => `${row.device_id}:${row.peer_id}:${row.profile}`}
              dataSource={relatedTunnels}
              pagination={false}
              columns={[
                { title: '设备', dataIndex: 'device_id' },
                { title: '对端', dataIndex: 'peer_id' },
                { title: '状态', dataIndex: 'state', render: (v) => <StatusTag state={v} /> },
                { title: '路径', dataIndex: 'via', render: (v) => v || '-' },
                { title: 'RTT', dataIndex: 'rtt_ms', render: (v) => v ? `${v} ms` : '-' },
                { title: 'NAT', dataIndex: 'nat_type', render: (v) => v || '-' },
                { title: '最近错误', dataIndex: 'last_error', render: (v) => v || '-' },
              ]}
            />
          </Card>
          <Card
            title="关联会话"
            size="small"
            extra={<Button size="small" type="link" onClick={() => { window.location.hash = '#/sessions' }}>打开会话页</Button>}
          >
            <Table
              size="small"
              rowKey="id"
              dataSource={relatedSessions}
              pagination={{ pageSize: 5 }}
              columns={[
                { title: 'ID', dataIndex: 'id' },
                { title: '路径', dataIndex: 'path', render: (v) => v || '-' },
                { title: '中继流量', dataIndex: 'relay_bytes' },
                { title: '开始时间', dataIndex: 'started_at', render: formatTime },
                { title: '最近活跃', dataIndex: 'last_seen', render: formatTime },
              ]}
            />
          </Card>
        </Space>
      ) : null}
    </Drawer>
  )
}

function formatTime(value?: string) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-'
}
