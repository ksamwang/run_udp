import type { AdminUser, AuthResponse } from '../types/api'

const ACCESS_TOKEN_KEY = 'udp_tunnel_access_token'
const ACCESS_EXPIRES_KEY = 'udp_tunnel_access_expires_at'
const REFRESH_TOKEN_KEY = 'udp_tunnel_refresh_token'
const REFRESH_EXPIRES_KEY = 'udp_tunnel_refresh_expires_at'

export function getAccessToken() {
  return localStorage.getItem(ACCESS_TOKEN_KEY) || ''
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_TOKEN_KEY) || ''
}

export function getAccessExpiresAt() {
  return localStorage.getItem(ACCESS_EXPIRES_KEY) || ''
}

export function saveAuth(resp: AuthResponse) {
  localStorage.setItem(ACCESS_TOKEN_KEY, resp.access_token)
  localStorage.setItem(ACCESS_EXPIRES_KEY, resp.access_expires_at)
  if (resp.refresh_token) {
    localStorage.setItem(REFRESH_TOKEN_KEY, resp.refresh_token)
  }
  if (resp.refresh_expires_at) {
    localStorage.setItem(REFRESH_EXPIRES_KEY, resp.refresh_expires_at)
  }
}

export function clearAuth() {
  localStorage.removeItem(ACCESS_TOKEN_KEY)
  localStorage.removeItem(ACCESS_EXPIRES_KEY)
  localStorage.removeItem(REFRESH_TOKEN_KEY)
  localStorage.removeItem(REFRESH_EXPIRES_KEY)
}

export function hasAuth() {
  return Boolean(getAccessToken() && getRefreshToken())
}

export function authUser(resp: AuthResponse): AdminUser {
  return resp.user
}
