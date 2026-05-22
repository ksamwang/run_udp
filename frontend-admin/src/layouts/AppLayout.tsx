import { DashboardOutlined, HistoryOutlined, LogoutOutlined, SettingOutlined, SwapOutlined, TeamOutlined } from '@ant-design/icons'
import { Button, Layout, Menu, Space, Typography } from 'antd'
import type { ReactNode } from 'react'

const { Header, Sider, Content } = Layout

type AppLayoutProps = {
  children: ReactNode
  activePage: string
  pageTitle: string
  onPageChange: (page: string) => void
  onLogout: () => void
}

export function AppLayout({ children, activePage, pageTitle, onPageChange, onLogout }: AppLayoutProps) {
  return (
    <Layout className="app-shell">
      <Sider width={232} className="app-sider">
        <div className="brand">
          <Typography.Text className="brand-title">UDP Tunnel</Typography.Text>
          <Typography.Text className="brand-subtitle">Admin Console</Typography.Text>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[activePage]}
          onClick={({ key }) => onPageChange(key)}
          items={[
            { key: 'dashboard', icon: <DashboardOutlined />, label: '总览' },
            { key: 'devices', icon: <TeamOutlined />, label: '设备' },
            { key: 'rules', icon: <SwapOutlined />, label: '转发规则' },
            { key: 'sessions', icon: <HistoryOutlined />, label: '会话' },
            { key: 'settings', icon: <SettingOutlined />, label: '设置' },
          ]}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <Typography.Title level={4} className="page-title">
            {pageTitle}
          </Typography.Title>
          <Space>
            <Typography.Text type="secondary">admin</Typography.Text>
            <Button icon={<LogoutOutlined />} onClick={onLogout}>
              退出
            </Button>
          </Space>
        </Header>
        <Content className="app-content">{children}</Content>
      </Layout>
    </Layout>
  )
}
