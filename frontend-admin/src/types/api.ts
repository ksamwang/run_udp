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

export type Device = {
  id: string
  name: string
  addr: string
  upnp_addr?: string
  want?: string
  online: boolean
  enabled: boolean
  last_seen: string
  created_at: string
  health_summary?: string
  last_error?: string
}

export type ForwardRule = {
  id: number
  name: string
  source_id: string
  target_id: string
  profile: string
  local_port: number
  target_host: string
  target_port: number
  enabled: boolean
  created_at: string
  updated_at: string
  runtime_state?: string
  last_error?: string
  last_updated_at?: string
  attempt?: number
  next_retry_at?: string
}

export type ForwardRulePayload = {
  name: string
  source_id: string
  target_id: string
  profile: string
  local_port: number
  target_host: string
  target_port: number
  enabled: boolean
}

export type TunnelState = {
  device_id: string
  peer_id: string
  profile: string
  state: string
  via: string
  nat_type: string
  public_addr: string
  conv_id: number
  rtt_ms: number
  last_error: string
  attempt: number
  next_retry_at: string
  last_transition_at: string
  updated_at: string
}

export type Session = {
  id: number
  source_id: string
  target_id: string
  profile: string
  path: string
  relay_bytes: number
  started_at: string
  last_seen: string
  ended_at?: string
}

export type Settings = {
  udp_listen: string
  stun_alt_listen: string
  http_listen: string
  database_path: string
  psk_configured: boolean
  peer_ttl: string
  pair_ttl: string
  relay_idle_timeout: string
  allow_relay: boolean
  allow_legacy: boolean
  client_no_upnp: boolean
  client_upnp_timeout: string
  client_log_level: string
  client_tray_enabled: boolean
  client_punch_timeout: string
  client_force_relay: boolean
  client_allow_legacy: boolean
  client_release_version: string
  client_release_url: string
  client_release_sha256: string
  client_release_published_at: string
  client_release_notes: string
  client_release_minimum_supported_version: string
  client_release_file: string
  restart_only_fields?: string[]
}
