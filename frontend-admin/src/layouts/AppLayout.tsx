import { ApartmentOutlined, AuditOutlined, DashboardOutlined, HistoryOutlined, LogoutOutlined, SettingOutlined, SwapOutlined, TeamOutlined } from '@ant-design/icons'
import { Button, Layout, Menu, Space, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { AdminUser } from '../types/api'

const { Header, Sider, Content } = Layout

type AppLayoutProps = {
  children: ReactNode
  activePage: string
  pageTitle: string
  user?: AdminUser
  lockedToSettings?: boolean
  onPageChange: (page: string) => void
  onLogout: () => void
}

export function AppLayout({ children, activePage, pageTitle, user, lockedToSettings, onPageChange, onLogout }: AppLayoutProps) {
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
            { key: 'dashboard', icon: <DashboardOutlined />, label: '总览', disabled: Boolean(lockedToSettings) },
            { key: 'devices', icon: <TeamOutlined />, label: '设备', disabled: Boolean(lockedToSettings) },
            { key: 'rules', icon: <SwapOutlined />, label: 'Agent 转发规则', disabled: Boolean(lockedToSettings) },
            { key: 'sessions', icon: <HistoryOutlined />, label: '会话', disabled: Boolean(lockedToSettings) },
            { key: 'lan', icon: <ApartmentOutlined />, label: '虚拟局域网', disabled: Boolean(lockedToSettings) },
            { key: 'audit', icon: <AuditOutlined />, label: '审计日志', disabled: Boolean(lockedToSettings) },
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
            <Typography.Text type="secondary">{user?.name || user?.username || 'admin'}</Typography.Text>
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
