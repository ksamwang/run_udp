import { App as AntApp, Spin } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { HashRouter, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { clearAuth, hasAuth } from './api/auth'
import { getMe } from './api/metrics'
import { AUTH_EXPIRED_EVENT, AuthExpiredError, logout } from './api/client'
import { AppLayout } from './layouts/AppLayout'
import { LoginPage } from './pages/LoginPage'

const pageTitles: Record<string, string> = {
  dashboard: '总览',
  devices: '设备',
  rules: '转发规则',
  sessions: '会话',
  lan: '虚拟局域网',
  settings: '设置',
}

export default function App() {
  return (
    <HashRouter>
      <AdminApp />
    </HashRouter>
  )
}

const DashboardPage = lazy(() => import('./pages/DashboardPage').then((m) => ({ default: m.DashboardPage })))
const DevicesPage = lazy(() => import('./pages/DevicesPage').then((m) => ({ default: m.DevicesPage })))
const RulesPage = lazy(() => import('./pages/RulesPage').then((m) => ({ default: m.RulesPage })))
const SessionsPage = lazy(() => import('./pages/SessionsPage').then((m) => ({ default: m.SessionsPage })))
const LanPage = lazy(() => import('./pages/LanPage').then((m) => ({ default: m.LanPage })))
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))

function AdminApp() {
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const [authenticated, setAuthenticated] = useState(hasAuth())
  const [sessionMessage, setSessionMessage] = useState('')
  const activePage = useMemo(() => pageFromPath(location.pathname), [location.pathname])
  const me = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    enabled: authenticated,
    retry: false,
  })
  const forcePasswordChange = Boolean(me.data?.user.force_password_change)

  useEffect(() => {
    if (!me.isError) {
      return
    }
    if (me.error instanceof AuthExpiredError) {
      setSessionMessage(me.error.message)
      clearAuth()
      queryClient.clear()
      setAuthenticated(false)
      return
    }
    setSessionMessage('登录状态异常，请重新登录')
    clearAuth()
    queryClient.clear()
    setAuthenticated(false)
  }, [me.error, me.isError, queryClient])

  useEffect(() => {
    function handleAuthExpired(event: Event) {
      const detail = (event as CustomEvent<{ message?: string }>).detail
      setSessionMessage(detail?.message || '登录状态已失效，请重新登录')
      queryClient.clear()
      setAuthenticated(false)
    }
    window.addEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired)
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, handleAuthExpired)
  }, [queryClient])

  if (!authenticated) {
    return (
      <AntApp>
        <LoginPage
          sessionMessage={sessionMessage || (me.error instanceof AuthExpiredError ? me.error.message : undefined)}
          onLoggedIn={() => {
            setSessionMessage('')
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

  if (me.isError) return <Spin fullscreen tip="正在退出登录" />

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
            <Route path="/lan" element={<LanPage />} />
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
