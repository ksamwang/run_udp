export type AdminUser = {
  id: string
  name: string
  role: string
}

export type AuthResponse = {
  access_token: string
  access_expires_at: string
  refresh_token?: string
  refresh_expires_at?: string
  user: AdminUser
}

export type Metrics = {
  devices: number
  online_devices: number
  forward_rules: number
  active_sessions: number
  relay_bytes: number
}

export type HealthResponse = {
  status: string
  uptime_seconds: number
  total_register: number
  total_paired: number
  total_relayed_bytes: number
  current_peers: number
  metrics: Metrics
  server_time: string
}
