import { LockOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { login } from '../api/client'

type LoginPageProps = {
  onLoggedIn: () => void
}

export function LoginPage({ onLoggedIn }: LoginPageProps) {
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(values: { password: string }) {
    setError('')
    setLoading(true)
    try {
      await login(values.password)
      onLoggedIn()
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
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
          {error && <Alert type="error" message={error} showIcon className="form-alert" />}
          <Form layout="vertical" onFinish={submit}>
            <Form.Item name="password" label="管理员密码" rules={[{ required: true, message: '请输入管理员密码' }]}>
              <Input.Password prefix={<LockOutlined />} autoFocus />
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
