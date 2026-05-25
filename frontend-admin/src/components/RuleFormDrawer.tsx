import { Button, Drawer, Form, Input, InputNumber, Select, Segmented, Switch, Typography } from 'antd'
import { useEffect } from 'react'
import type { Device, ForwardRule, ForwardRulePayload } from '../types/api'

type RuleFormDrawerProps = {
  open: boolean
  devices: Device[]
  rule?: ForwardRule | null
  submitting: boolean
  onClose: () => void
  onSubmit: (payload: ForwardRulePayload) => void
}

export function RuleFormDrawer({ open, devices, rule, submitting, onClose, onSubmit }: RuleFormDrawerProps) {
  const [form] = Form.useForm<ForwardRulePayload>()

  useEffect(() => {
    if (!open) {
      return
    }
    form.setFieldsValue(rule ? ruleToPayload(rule) : {
      name: '',
      source_id: '',
      target_id: '',
      profile: 'interactive',
      local_port: 13389,
      target_host: '127.0.0.1',
      target_port: 3389,
      enabled: true,
    })
  }, [form, open, rule])

  const deviceOptions = devices.map((d) => ({
    value: d.id,
    label: `${d.name || d.id} (${d.online ? '在线' : '离线'})`,
  }))

  return (
    <Drawer
      title={rule ? '编辑 Agent 转发规则' : '新增 Agent 转发规则'}
      open={open}
      onClose={onClose}
      width={520}
      destroyOnClose
      extra={
        <Button type="primary" loading={submitting} disabled={submitting} onClick={() => form.submit()}>
          保存
        </Button>
      }
    >
      <Form form={form} layout="vertical" onFinish={onSubmit}>
        <Form.Item name="name" label="规则名" rules={[{ required: true, message: '请输入规则名' }]}>
          <Input placeholder="例如 office-rdp" />
        </Form.Item>
        <Typography.Text type="secondary">该规则只适用于老 Agent 客户端，入口和出口设备都需要运行 Agent。</Typography.Text>
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item name="profile" label="连接模式" rules={[{ required: true }]}>
          <Segmented block options={[{ label: '交互优先', value: 'interactive' }, { label: '吞吐优先', value: 'bulk' }]} />
        </Form.Item>
        <Form.Item
          name="source_id"
          label="入口设备"
          rules={[{ required: true, message: '请选择入口设备' }]}
        >
          <Select showSearch options={deviceOptions} optionFilterProp="label" />
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev, cur) => prev.source_id !== cur.source_id}
        >
          {({ getFieldValue }) => (
            <Form.Item
              name="target_id"
              label="出口设备"
              rules={[
                { required: true, message: '请选择出口设备' },
                {
                  validator: (_, value) => {
                    if (!value || value !== getFieldValue('source_id')) return Promise.resolve()
                    return Promise.reject(new Error('入口设备和出口设备不能相同'))
                  },
                },
              ]}
            >
              <Select showSearch options={deviceOptions} optionFilterProp="label" />
            </Form.Item>
          )}
        </Form.Item>
        <Form.Item name="local_port" label="本地端口" rules={[{ required: true, type: 'number', min: 1, max: 65535 }]}>
          <InputNumber min={1} max={65535} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="target_host" label="目标 Host" rules={[{ required: true, message: '请输入目标 Host' }]}>
          <Input placeholder="127.0.0.1" />
        </Form.Item>
        <Form.Item name="target_port" label="目标端口" rules={[{ required: true, type: 'number', min: 1, max: 65535 }]}>
          <InputNumber min={1} max={65535} precision={0} style={{ width: '100%' }} />
        </Form.Item>
      </Form>
    </Drawer>
  )
}

function ruleToPayload(rule: ForwardRule): ForwardRulePayload {
  return {
    name: rule.name,
    source_id: rule.source_id,
    target_id: rule.target_id,
    profile: rule.profile || 'interactive',
    local_port: rule.local_port,
    target_host: rule.target_host,
    target_port: rule.target_port,
    enabled: rule.enabled,
  }
}
