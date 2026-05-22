import { SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, message, Select, Space, Switch, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { changePassword, getSettings, updateSettings } from '../api/settings'
import type { Settings } from '../types/api'

export function SettingsPage() {
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
    },
    onError: (err) => message.error(err instanceof Error ? err.message : '修改密码失败'),
  })

  useEffect(() => {
    if (settings.data) {
      form.setFieldsValue(settings.data)
    }
  }, [form, settings.data])

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
      <Alert type="info" showIcon message="监听地址、数据库路径、PSK 等启动参数仍需要修改配置文件并重启服务。" />
      <Form form={form} layout="vertical" onFinish={(values) => saveMutation.mutate(values)}>
        <Card title="隧道策略">
          <div className="settings-grid">
            <Form.Item name="peer_ttl" label="Peer TTL" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="pair_ttl" label="Pair TTL" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="relay_idle_timeout" label="Relay Idle Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="allow_relay" label="允许 Relay" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="allow_legacy" label="允许 Legacy 明文协议" valuePropName="checked"><Switch /></Form.Item>
          </div>
        </Card>
        <Card title="客户端默认配置">
          <div className="settings-grid">
            <Form.Item name="client_no_upnp" label="禁用 UPnP" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="client_upnp_timeout" label="UPnP Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="client_punch_timeout" label="Punch Timeout" rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="client_force_relay" label="强制 Relay" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="client_allow_legacy" label="客户端允许 Legacy" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="client_log_level" label="日志等级">
              <Select options={['debug', 'info', 'warn', 'error'].map((v) => ({ value: v, label: v }))} />
            </Form.Item>
            <Form.Item name="client_tray_enabled" label="启用托盘" valuePropName="checked"><Switch /></Form.Item>
          </div>
        </Card>
        <Card title="客户端发布">
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
      </Form>
      <Card title="管理员密码">
        <Form
          form={passwordForm}
          layout="vertical"
          onFinish={(values) => passwordMutation.mutate({ current_password: values.current_password, new_password: values.new_password })}
        >
          <div className="settings-grid">
            <Form.Item name="current_password" label="当前密码" rules={[{ required: true }]}><Input.Password /></Form.Item>
            <Form.Item name="new_password" label="新密码" rules={[{ required: true, min: 8 }]}><Input.Password /></Form.Item>
            <Form.Item
              name="confirm_password"
              label="确认新密码"
              dependencies={['new_password']}
              rules={[
                { required: true },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('new_password') === value) return Promise.resolve()
                    return Promise.reject(new Error('两次输入的密码不一致'))
                  },
                }),
              ]}
            >
              <Input.Password />
            </Form.Item>
          </div>
          <Space>
            <Button type="primary" htmlType="submit" loading={passwordMutation.isPending}>修改密码</Button>
          </Space>
        </Form>
      </Card>
    </div>
  )
}
