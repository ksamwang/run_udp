import { App as AntApp, Spin } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { lazy, Suspense, useMemo, useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { clearAuth, hasAuth } from './api/auth'
import { getMe } from './api/metrics'
import { logout } from './api/client'
import { AppLayout } from './layouts/AppLayout'
import { LoginPage } from './pages/LoginPage'

const pageTitles: Record<string, string> = {
  dashboard: '总览',
  devices: '设备',
  rules: '转发规则',
  sessions: '会话',
  settings: '设置',
}

export default function App() {
  return (
    <BrowserRouter>
      <AdminApp />
    </BrowserRouter>
  )
}

const DashboardPage = lazy(() => import('./pages/DashboardPage').then((m) => ({ default: m.DashboardPage })))
const DevicesPage = lazy(() => import('./pages/DevicesPage').then((m) => ({ default: m.DevicesPage })))
const RulesPage = lazy(() => import('./pages/RulesPage').then((m) => ({ default: m.RulesPage })))
const SessionsPage = lazy(() => import('./pages/SessionsPage').then((m) => ({ default: m.SessionsPage })))
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))

function AdminApp() {
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const [authenticated, setAuthenticated] = useState(hasAuth())
  const activePage = useMemo(() => pageFromPath(location.pathname), [location.pathname])
  const me = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    enabled: authenticated,
    retry: false,
  })
  const forcePasswordChange = Boolean(me.data?.user.force_password_change)

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

  if (forcePasswordChange && activePage !== 'settings') {
    return <Navigate to="/settings" replace />
  }

  return (
    <AntApp>
      <AppLayout
        activePage={activePage}
        pageTitle={pageTitles[activePage] || '总览'}
        user={me.data?.user}
        lockedToSettings={forcePasswordChange}
        onPageChange={(page) => navigate(pathFromPage(page))}
        onLogout={async () => {
          await logout()
          queryClient.clear()
          setAuthenticated(false)
        }}
      >
        <Suspense fallback={<Spin tip="正在加载页面" />}>
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/devices" element={<DevicesPage />} />
            <Route path="/rules" element={<RulesPage />} />
            <Route path="/sessions" element={<SessionsPage />} />
            <Route path="/settings" element={<SettingsPage forcePasswordChange={forcePasswordChange} />} />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Routes>
        </Suspense>
      </AppLayout>
    </AntApp>
  )
}

function pageFromPath(pathname: string) {
  const page = pathname.split('/').filter(Boolean)[0] || 'dashboard'
  return pageTitles[page] ? page : 'dashboard'
}

function pathFromPage(page: string) {
  return `/${page}`
}
