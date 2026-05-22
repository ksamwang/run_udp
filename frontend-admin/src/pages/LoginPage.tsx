import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { login } from '../api/client'

type LoginPageProps = {
  sessionMessage?: string
  onLoggedIn: () => void
}

export function LoginPage({ sessionMessage, onLoggedIn }: LoginPageProps) {
  const [form] = Form.useForm<{ username: string; password: string }>()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(values: { username: string; password: string }) {
    setError('')
    setLoading(true)
    try {
      await login(values.username, values.password)
      onLoggedIn()
    } catch (err) {
      form.setFieldValue('password', '')
      const message = err instanceof Error ? err.message : '登录失败'
      setError(message === 'unauthorized' ? '账号或密码错误' : message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="login-page">
      <section className="login-panel">
        <div className="login-copy">
          <Typography.Title>UDP Tunnel</Typography.Title>
          <Typography.Paragraph>
            管理设备、转发规则和隧道运行状态。
          </Typography.Paragraph>
        </div>
        <Card className="login-card" title="管理员登录">
          {sessionMessage ? <Alert type="warning" message={sessionMessage} showIcon className="form-alert" /> : null}
          {error && <Alert type="error" message={error} showIcon className="form-alert" />}
          <Form form={form} layout="vertical" initialValues={{ username: 'admin' }} onFinish={submit}>
            <Form.Item name="username" label="管理员账号" rules={[{ required: true, message: '请输入管理员账号' }]}>
              <Input prefix={<UserOutlined />} autoFocus />
            </Form.Item>
            <Form.Item name="password" label="管理员密码" rules={[{ required: true, message: '请输入管理员密码' }]}>
              <Input.Password prefix={<LockOutlined />} />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              登录
            </Button>
          </Form>
        </Card>
      </section>
    </main>
  )
}
