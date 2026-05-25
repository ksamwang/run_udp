export type AdminUser = {
  id: string
  username: string
  name: string
  role: string
  force_password_change?: boolean
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

export type AuditEvent = {
  id: number
  kind: string
  detail: string
  created_at: string
}

export type ReleaseUploadResponse = {
  product: string
  file: string
  url: string
  sha256: string
  uploaded_at: string
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
  control_database_configured: boolean
  psk_configured: boolean
  peer_ttl: string
  pair_ttl: string
  relay_idle_timeout: string
  allow_relay: boolean
  lan_allow_relay: boolean
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
  lan_release_version: string
  lan_release_url: string
  lan_release_sha256: string
  lan_release_published_at: string
  lan_release_notes: string
  lan_release_minimum_supported_version: string
  lan_release_file: string
  restart_only_fields?: string[]
}

export type VirtualNetwork = {
  id: number
  name: string
  cidr: string
  mtu: number
  mss: number
  path_policy: string
  tcp_fast_path: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export type VirtualAddress = {
  device_id: string
  network_id: number
  virtual_ip: string
  hostname: string
  dns_enabled: boolean
  created_at: string
  updated_at: string
}

export type VirtualDeviceKey = {
  device_id: string
  algorithm: string
  public_key: string
  created_at: string
  updated_at: string
}

export type VirtualDeviceGroup = {
  id: string
  name: string
  device_ids: string[]
  created_at: string
  updated_at: string
}

export type VirtualDeviceGroupPayload = Pick<VirtualDeviceGroup, 'id' | 'name' | 'device_ids'>

export type VirtualACLRule = {
  id: number
  network_id: number
  source_device_id: string
  source_group_id: string
  target_device_id: string
  target_group_id: string
  protocol: string
  port_start: number
  port_end: number
  action: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export type VirtualACLRulePayload = Omit<VirtualACLRule, 'id' | 'created_at' | 'updated_at'>

export type VirtualRoute = {
  id: number
  device_id: string
  network_id: number
  cidr: string
  advertise: boolean
  accept: boolean
  created_at: string
  updated_at: string
}

export type VirtualRoutePayload = Omit<VirtualRoute, 'id' | 'created_at' | 'updated_at'>

export type VirtualPeerState = {
  device_id: string
  peer_id: string
  network_id: number
  state: string
  path: string
  data_path: string
  path_reason: string
  nat_type: string
  fallback_reason: string
  traffic_class: string
  tcp_fast_path: string
  adapter_state: string
  route_conflict: string
  selected_cidr: string
  mtu: number
  mss: number
  rtt_ms: number
  estimated_bps: number
  tx_bytes: number
  rx_bytes: number
  drop_reason: string
  last_error: string
  last_handshake_at: string
  last_transition_at: string
  updated_at: string
}

export type VirtualPeerPathEvent = {
  id: number
  device_id: string
  peer_id: string
  network_id: number
  path: string
  data_path: string
  path_reason: string
  traffic_class: string
  tx_bytes: number
  rx_bytes: number
  created_at: string
}

export type VirtualLearnedPath = {
  device_id: string
  peer_id: string
  network_id: number
  dst_port: number
  protocol: string
  path: string
  public_addr: string
  success_count: number
  failure_count: number
  last_success_at: string
  last_failure_at: string
  last_failure: string
  quality: string
  preheat_enabled: boolean
  updated_at: string
}

export type VirtualDeviceState = {
  device_id: string
  network_id: number
  virtual_ip: string
  hostname: string
  adapter_state: string
  selected_cidr: string
  route_conflict: string
  p2p_peers: number
  relay_peers: number
  down_peers: number
  last_bootstrap_at: string
  last_status_at: string
  last_error: string
}
