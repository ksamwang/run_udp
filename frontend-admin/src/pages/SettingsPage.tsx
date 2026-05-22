import { ExclamationCircleOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, InputNumber, Modal, message, Select, Space, Switch, Tabs, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { changePassword, getSettings, updateSettings } from '../api/settings'
import type { Settings } from '../types/api'

const { confirm } = Modal

type SettingsPageProps = {
  forcePasswordChange?: boolean
}

type DurationUnit = 's' | 'm' | 'h'
type DurationInputValue = {
  amount?: number
  unit?: DurationUnit
}
type SettingsForm = Omit<Settings, 'peer_ttl' | 'pair_ttl' | 'relay_idle_timeout' | 'client_upnp_timeout' | 'client_punch_timeout'> & {
  peer_ttl: DurationInputValue
  pair_ttl: DurationInputValue
  relay_idle_timeout: DurationInputValue
  client_upnp_timeout: DurationInputValue
  client_punch_timeout: DurationInputValue
}

export function SettingsPage({ forcePasswordChange }: SettingsPageProps) {
  const [form] = Form.useForm<SettingsForm>()
  const [passwordForm] = Form.useForm<{ current_password: string; new_password: string; confirm_password: string }>()
  const queryClient = useQueryClient()
  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: getSettings,
  })
  const saveMutation = useMutation({
    mutationFn: updateSettings,
    onSuccess: () => {
      message.success('设置已保存')
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '保存失败'),
  })
  const passwordMutation = useMutation({
    mutationFn: changePassword,
    onSuccess: () => {
      message.success('管理员密码已更新')
      passwordForm.resetFields()
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '修改密码失败'),
  })
  async function submitPassword() {
    const values = passwordForm.getFieldsValue()
    if (!values.current_password || !values.new_password || !values.confirm_password) {
      await passwordForm.validateFields()
      return
    }
    if (values.new_password !== values.confirm_password) {
      message.error('两次输入的密码不一致')
      return
    }
    passwordMutation.mutate({ current_password: values.current_password, new_password: values.new_password })
  }
  function submitSettings() {
    form.validateFields()
      .then((values) => saveSettings(values as SettingsForm))
      .catch(() => undefined)
  }

  function saveSettings(values: SettingsForm) {
    const durationError = validateDurationValues(values)
    if (durationError) {
      message.error(durationError)
      return
    }
    const payload = formToSettings(values)
    const riskyTouched = payload.allow_legacy || payload.client_force_relay || payload.client_allow_legacy
    if (riskyTouched) {
      confirm({
        title: '确认保存高风险设置',
        content: '这些选项会放宽安全边界或改变连接策略，请确认你了解影响。',
        okText: '继续保存',
        cancelText: '取消',
        onOk: () => saveMutation.mutate(payload),
      })
      return
    }
    saveMutation.mutate(payload)
  }

  useEffect(() => {
    if (settings.data) {
      form.setFieldsValue(settingsToForm(settings.data))
    }
  }, [form, settings.data])

  const tabs = useMemo(() => [
    {
      key: 'runtime',
      label: '隧道策略',
      children: (
        <Card>
          <div className="settings-grid">
            <DurationField name="peer_ttl" label="设备心跳有效期" minSeconds={10} />
            <DurationField name="pair_ttl" label="配对请求有效期" minSeconds={10} />
            <DurationField name="relay_idle_timeout" label="中继空闲超时" minSeconds={10} />
            <Form.Item
              name="allow_relay"
              label="允许中继"
              valuePropName="checked"
              tooltip="关闭后，系统不会主动走中继路径。"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="allow_legacy"
              label="允许 Legacy 明文协议"
              valuePropName="checked"
              tooltip="启用后会放行旧协议，存在兼容和安全风险。"
            >
              <Switch />
            </Form.Item>
          </div>
        </Card>
      ),
    },
    {
      key: 'client',
      label: '客户端默认值',
      children: (
        <Card>
          <div className="settings-grid">
            <Form.Item name="client_no_upnp" label="禁用端口映射" valuePropName="checked"><Switch /></Form.Item>
            <DurationField name="client_upnp_timeout" label="端口映射超时" minSeconds={1} />
            <DurationField name="client_punch_timeout" label="打洞超时" minSeconds={1} />
            <Form.Item
              name="client_force_relay"
              label="强制中继"
              valuePropName="checked"
              tooltip="启用后客户端会优先走中继。"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="client_allow_legacy"
              label="客户端允许 Legacy"
              valuePropName="checked"
              tooltip="启用后客户端可接受旧协议，建议默认关闭。"
            >
              <Switch />
            </Form.Item>
            <Form.Item name="client_log_level" label="日志等级">
              <Select options={['debug', 'info', 'warn', 'error'].map((v) => ({ value: v, label: v }))} />
            </Form.Item>
            <Form.Item name="client_tray_enabled" label="启用托盘" valuePropName="checked"><Switch /></Form.Item>
          </div>
        </Card>
      ),
    },
    {
      key: 'release',
      label: '客户端发布',
      children: (
        <Card>
          <div className="settings-grid">
            <Form.Item name="client_release_version" label="版本号"><Input /></Form.Item>
            <Form.Item name="client_release_url" label="下载 URL"><Input /></Form.Item>
            <Form.Item name="client_release_sha256" label="SHA256"><Input /></Form.Item>
            <Form.Item name="client_release_published_at" label="发布时间"><Input /></Form.Item>
            <Form.Item name="client_release_minimum_supported_version" label="最低支持版本"><Input /></Form.Item>
            <Form.Item name="client_release_file" label="服务端安装包文件"><Input /></Form.Item>
            <Form.Item name="client_release_notes" label="发布说明" className="settings-wide"><Input.TextArea rows={4} /></Form.Item>
          </div>
        </Card>
      ),
    },
    {
      key: 'admin',
      label: '管理员密码',
      children: (
        <Card>
          <Form
            form={passwordForm}
            layout="vertical"
            onFinish={() => submitPassword()}
          >
            <div className="settings-grid">
              <Form.Item name="current_password" label="当前密码" rules={[{ required: true }]}><Input.Password /></Form.Item>
              <Form.Item name="new_password" label="新密码" rules={[{ required: true, min: 8 }]}><Input.Password /></Form.Item>
              <Form.Item
                name="confirm_password"
                label="确认新密码"
                rules={[{ required: true, message: '请确认新密码' }]}
              >
                <Input.Password />
              </Form.Item>
            </div>
            <Space>
              <Button type="primary" onClick={() => submitPassword()} loading={passwordMutation.isPending}>修改密码</Button>
            </Space>
          </Form>
        </Card>
      ),
    },
  ], [passwordForm, passwordMutation])

  return (
    <div className="page-stack">
      <div className="page-toolbar">
        <div>
          <Typography.Title level={3}>设置</Typography.Title>
          <Typography.Text type="secondary">管理隧道策略、客户端默认参数和发布信息。</Typography.Text>
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={saveMutation.isPending} onClick={submitSettings}>
          保存设置
        </Button>
      </div>
      <Alert
        type="info"
        showIcon
        message="监听地址、数据库路径、PSK 等启动参数仍需要修改配置文件并重启服务。"
      />
      {forcePasswordChange ? (
        <Alert
          type="warning"
          showIcon
          icon={<ExclamationCircleOutlined />}
          message="当前账号是默认管理员，首次登录后需要先修改密码。"
        />
      ) : null}
      <Form form={form} layout="vertical">
        <Tabs items={tabs} />
      </Form>
    </div>
  )
}

function DurationField({ name, label, minSeconds }: { name: keyof SettingsForm; label: string; minSeconds: number }) {
  const unitOptions = [
    { value: 's', label: '秒' },
    { value: 'm', label: '分钟' },
    { value: 'h', label: '小时' },
  ] satisfies Array<{ value: DurationUnit; label: string }>
  return (
    <Form.Item
      name={name}
      label={label}
      required
      validateStatus={undefined}
      rules={[
        {
          validator: (_, value: DurationInputValue) => {
            if (toSeconds(value) < minSeconds) {
              return Promise.reject(new Error(`不能小于 ${formatMinSeconds(minSeconds)}`))
            }
            return Promise.resolve()
          },
        },
      ]}
    >
      <DurationInput unitOptions={unitOptions} />
    </Form.Item>
  )
}

function DurationInput({
  value,
  onChange,
  unitOptions,
}: {
  value?: DurationInputValue
  onChange?: (value: DurationInputValue) => void
  unitOptions: Array<{ value: DurationUnit; label: string }>
}) {
  const current = value || { amount: undefined, unit: 's' as DurationUnit }
  return (
    <Space.Compact block>
      <InputNumber
        min={1}
        precision={0}
        value={current.amount}
        onChange={(amount) => onChange?.({ ...current, amount: amount || undefined })}
        style={{ width: '65%' }}
      />
      <Select
        value={current.unit || 's'}
        options={unitOptions}
        onChange={(unit) => onChange?.({ ...current, unit })}
        style={{ width: '35%' }}
      />
    </Space.Compact>
  )
}

function settingsToForm(settings: Settings): SettingsForm {
  return {
    ...settings,
    peer_ttl: parseDuration(settings.peer_ttl),
    pair_ttl: parseDuration(settings.pair_ttl),
    relay_idle_timeout: parseDuration(settings.relay_idle_timeout),
    client_upnp_timeout: parseDuration(settings.client_upnp_timeout),
    client_punch_timeout: parseDuration(settings.client_punch_timeout),
  }
}

function formToSettings(values: SettingsForm): Settings {
  return {
    ...values,
    peer_ttl: formatDuration(values.peer_ttl),
    pair_ttl: formatDuration(values.pair_ttl),
    relay_idle_timeout: formatDuration(values.relay_idle_timeout),
    client_upnp_timeout: formatDuration(values.client_upnp_timeout),
    client_punch_timeout: formatDuration(values.client_punch_timeout),
  }
}

function validateDurationValues(values: SettingsForm): string {
  const checks: Array<[keyof SettingsForm, string, number]> = [
    ['peer_ttl', '设备心跳有效期', 10],
    ['pair_ttl', '配对请求有效期', 10],
    ['relay_idle_timeout', '中继空闲超时', 10],
    ['client_upnp_timeout', '端口映射超时', 1],
    ['client_punch_timeout', '打洞超时', 1],
  ]
  for (const [key, label, minSeconds] of checks) {
    const seconds = toSeconds(values[key] as DurationInputValue)
    if (seconds < minSeconds) {
      return `${label} 不能小于 ${formatMinSeconds(minSeconds)}`
    }
  }
  return ''
}

function parseDuration(value: string): DurationInputValue {
  const match = /^(\d+)(s|m|h)(\d+s)?$/.exec(value || '')
  if (!match) {
    return { amount: 1, unit: 's' }
  }
  const amount = Number(match[1])
  const unit = match[2] as DurationUnit
  if (!match[3]) {
    return { amount, unit }
  }
  const seconds = toSeconds({ amount, unit }) + Number(match[3].slice(0, -1))
  if (seconds % 3600 === 0) return { amount: seconds / 3600, unit: 'h' }
  if (seconds % 60 === 0) return { amount: seconds / 60, unit: 'm' }
  return { amount: seconds, unit: 's' }
}

function formatMinSeconds(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function formatDuration(value: DurationInputValue): string {
  const amount = Math.max(1, Math.floor(value.amount || 1))
  return `${amount}${value.unit || 's'}`
}

function toSeconds(value: DurationInputValue): number {
  const amount = value.amount || 0
  switch (value.unit || 's') {
    case 'h':
      return amount * 3600
    case 'm':
      return amount * 60
    default:
      return amount
  }
}
