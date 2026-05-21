import { App as AntApp, Spin } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { clearAuth, hasAuth } from './api/auth'
import { getMe } from './api/metrics'
import { logout } from './api/client'
import { AppLayout } from './layouts/AppLayout'
import { DashboardPage } from './pages/DashboardPage'
import { DevicesPage } from './pages/DevicesPage'
import { LoginPage } from './pages/LoginPage'
import { RulesPage } from './pages/RulesPage'

const pageTitles: Record<string, string> = {
  dashboard: '总览',
  devices: '设备',
  rules: '转发规则',
  settings: '设置',
}

export default function App() {
  const queryClient = useQueryClient()
  const [authenticated, setAuthenticated] = useState(hasAuth())
  const [activePage, setActivePage] = useState('dashboard')
  const me = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    enabled: authenticated,
    retry: false,
  })

  if (!authenticated) {
    return (
      <AntApp>
        <LoginPage
          onLoggedIn={() => {
            setAuthenticated(true)
            queryClient.invalidateQueries({ queryKey: ['me'] })
          }}
        />
      </AntApp>
    )
  }

  if (me.isLoading) {
    return <Spin fullscreen tip="正在加载控制台" />
  }

  if (me.isError) {
    clearAuth()
    setAuthenticated(false)
    return null
  }

  return (
    <AntApp>
      <AppLayout
        activePage={activePage}
        pageTitle={pageTitles[activePage] || '总览'}
        onPageChange={setActivePage}
        onLogout={async () => {
          await logout()
          queryClient.clear()
          setAuthenticated(false)
        }}
      >
        {activePage === 'dashboard' && <DashboardPage />}
        {activePage === 'devices' && <DevicesPage />}
        {activePage === 'rules' && <RulesPage />}
        {activePage === 'settings' && (
          <div className="page-stack">
            <h2>设置</h2>
            <p>设置页将在下一阶段接入。</p>
          </div>
        )}
      </AppLayout>
    </AntApp>
  )
}
