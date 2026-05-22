import { ExclamationCircleOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Modal, message, Select, Space, Switch, Tabs, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { changePassword, getSettings, updateSettings } from '../api/settings'
import type { Settings } from '../types/api'

const { confirm } = Modal

type SettingsPageProps = {
  forcePasswordChange?: boolean
}

export function SettingsPage({ forcePasswordChange }: SettingsPageProps) {
  const [form] = Form.useForm<Settings>()
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

  useEffect(() => {
    if (settings.data) {
      form.setFieldsValue(settings.data)
    }
  }, [form, settings.data])

  const tabs = useMemo(() => [
    {
      key: 'runtime',
      label: '隧道策略',
      children: (
        <Card>
          <div className="settings-grid">
            <Form.Item name="peer_ttl" label="Peer TTL" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="pair_ttl" label="Pair TTL" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="relay_idle_timeout" label="Relay Idle Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item
              name="allow_relay"
              label="允许 Relay"
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
            <Form.Item name="client_no_upnp" label="禁用 UPnP" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="client_upnp_timeout" label="UPnP Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="client_punch_timeout" label="Punch Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item
              name="client_force_relay"
              label="强制 Relay"
              valuePropName="checked"
              tooltip="启用后客户端会优先走 Relay。"
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
            <Form.Item name="client_release_notes" label="Release Notes" className="settings-wide"><Input.TextArea rows={4} /></Form.Item>
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
        <Button type="primary" icon={<SaveOutlined />} loading={saveMutation.isPending} onClick={() => form.submit()}>
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
      <Form form={form} layout="vertical" onFinish={(values) => {
        const riskyTouched = values.allow_legacy || values.client_force_relay || values.client_allow_legacy
        if (riskyTouched) {
          confirm({
            title: '确认保存高风险设置',
            content: '这些选项会放宽安全边界或改变连接策略，请确认你了解影响。',
            okText: '继续保存',
            cancelText: '取消',
            onOk: () => saveMutation.mutate(values),
          })
          return
        }
        saveMutation.mutate(values)
      }}>
        <Tabs items={tabs} />
      </Form>
    </div>
  )
}
