import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, message, Popconfirm, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { listDevices } from '../api/devices'
import { createRule, deleteRule, listRules, setRuleEnabled, updateRule } from '../api/rules'
import { RuleFormDrawer } from '../components/RuleFormDrawer'
import { StatusTag } from '../components/StatusTag'
import type { ForwardRule, ForwardRulePayload } from '../types/api'

export function RulesPage() {
  const queryClient = useQueryClient()
  const [editingRule, setEditingRule] = useState<ForwardRule | null>(null)
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
          <Typography.Title level={3}>转发规则</Typography.Title>
          <Typography.Text type="secondary">查看入口设备、本地端口、出口目标和当前运行状态。</Typography.Text>
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => rules.refetch()} loading={rules.isFetching}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditingRule(null); setDrawerOpen(true) }}>新增规则</Button>
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
                  <Tooltip title="编辑规则">
                    <Button size="small" icon={<EditOutlined />} onClick={() => { setEditingRule(r); setDrawerOpen(true) }} />
                  </Tooltip>
                  <Popconfirm title="删除规则" description="确认删除这条转发规则？" onConfirm={() => deleteMutation.mutate(r.id)}>
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
    </div>
  )
}

function formatProfile(profile?: string) {
  return profile === 'bulk' ? '吞吐优先' : '交互优先'
}
