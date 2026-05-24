import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Button, Card, DatePicker, Form, Input, Select, Space, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useState } from 'react'
import { listAuditEvents, type AuditQuery } from '../api/audit'

const { RangePicker } = DatePicker

type AuditFilterForm = {
  kind?: string
  keyword?: string
  range?: [dayjs.Dayjs, dayjs.Dayjs]
}

const commonKinds = [
  'device_set_enabled',
  'device_delete',
  'rule_create',
  'rule_update',
  'rule_delete',
]

export function AuditPage() {
  const [form] = Form.useForm<AuditFilterForm>()
  const [query, setQuery] = useState<AuditQuery>({ limit: 200 })
  const audit = useQuery({
    queryKey: ['audit-events', query],
    queryFn: () => listAuditEvents(query),
  })

  function applyFilters() {
    const values = form.getFieldsValue()
    setQuery({
      kind: values.kind,
      keyword: values.keyword?.trim(),
      from: values.range?.[0]?.startOf('day').toISOString(),
      to: values.range?.[1]?.endOf('day').toISOString(),
      limit: 200,
    })
  }

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>审计日志</Typography.Title>
          <Typography.Text type="secondary">查看管理操作记录和变更详情。</Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={() => audit.refetch()} loading={audit.isFetching}>刷新</Button>
      </div>
      <Card>
        <Form form={form} layout="inline" onFinish={applyFilters}>
          <Form.Item name="kind" label="类型">
            <Select
              allowClear
              showSearch
              placeholder="全部类型"
              style={{ width: 220 }}
              options={commonKinds.map((kind) => ({ value: kind, label: kind }))}
            />
          </Form.Item>
          <Form.Item name="keyword" label="关键字">
            <Input allowClear placeholder="搜索类型或详情" />
          </Form.Item>
          <Form.Item name="range" label="时间">
            <RangePicker />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" icon={<SearchOutlined />} htmlType="submit">筛选</Button>
              <Button
                onClick={() => {
                  form.resetFields()
                  setQuery({ limit: 200 })
                }}
              >
                重置
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
      <Card>
        <Table
          rowKey="id"
          loading={audit.isLoading}
          dataSource={audit.data || []}
          columns={[
            { title: 'ID', dataIndex: 'id', width: 90 },
            { title: '操作类型', dataIndex: 'kind', width: 220, render: (v) => <Tag>{v}</Tag> },
            { title: '详情', dataIndex: 'detail', render: (v) => <Typography.Text copyable>{v}</Typography.Text> },
            { title: '时间', dataIndex: 'created_at', width: 190, render: (v) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-' },
          ]}
          scroll={{ x: 900 }}
        />
      </Card>
    </div>
  )
}
