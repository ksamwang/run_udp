import { App as AntApp, Spin } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { clearAuth, hasAuth } from './api/auth'
import { getMe } from './api/metrics'
import { logout } from './api/client'
import { AppLayout } from './layouts/AppLayout'
import { DashboardPage } from './pages/DashboardPage'
import { LoginPage } from './pages/LoginPage'

export default function App() {
  const queryClient = useQueryClient()
  const [authenticated, setAuthenticated] = useState(hasAuth())
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
        onLogout={async () => {
          await logout()
          queryClient.clear()
          setAuthenticated(false)
        }}
      >
        <DashboardPage />
      </AppLayout>
    </AntApp>
  )
}
