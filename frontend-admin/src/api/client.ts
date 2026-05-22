import { clearAuth, getAccessExpiresAt, getAccessToken, getRefreshToken, saveAuth } from './auth'
import type { AuthResponse } from '../types/api'

const API_BASE = import.meta.env.VITE_API_BASE_URL || ''

let refreshPromise: Promise<AuthResponse> | null = null

export class AuthExpiredError extends Error {
  constructor(message = '登录状态已失效，请重新登录') {
    super(message)
    this.name = 'AuthExpiredError'
  }
}

export const AUTH_EXPIRED_EVENT = 'udp-tunnel-auth-expired'

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  await refreshIfNeeded()
  const res = await send(path, init)
  if (res.status !== 401) {
    return readJSON<T>(res)
  }
  if (!shouldRefreshAfter401(path)) {
    throwAuthExpired()
  }
  await refreshTokens()
  const retry = await send(path, init)
  if (retry.status === 401) {
    throwAuthExpired()
  }
  return readJSON<T>(retry)
}

export async function login(username: string, password: string) {
  const res = await fetch(`${API_BASE}/api/admin/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  const data = await readJSON<AuthResponse>(res)
  saveAuth(data)
  return data
}

export async function logout() {
  const refreshToken = getRefreshToken()
  try {
    if (refreshToken) {
      await fetch(`${API_BASE}/api/admin/auth/logout`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      })
    }
  } finally {
    clearAuth()
  }
}

async function send(path: string, init: RequestInit) {
  const headers = new Headers(init.headers)
  const token = getAccessToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  return fetch(`${API_BASE}${path}`, { ...init, headers })
}

async function refreshIfNeeded() {
  const expiresAt = getAccessExpiresAt()
  if (!expiresAt) {
    return
  }
  const expires = new Date(expiresAt).getTime()
  if (Number.isNaN(expires) || expires - Date.now() > 2 * 60 * 1000) {
    return
  }
  await refreshTokens()
}

async function refreshTokens() {
  if (refreshPromise) {
    return refreshPromise
  }
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    throwAuthExpired('登录已过期，请重新登录')
  }
  refreshPromise = fetch(`${API_BASE}/api/admin/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })
    .then((res) => readJSON<AuthResponse>(res))
    .then((data) => {
      saveAuth(data)
      return data
    })
    .catch((err) => {
      throwAuthExpired(err instanceof Error ? err.message : '登录已过期，请重新登录')
    })
    .finally(() => {
      refreshPromise = null
    })
  return refreshPromise
}

async function readJSON<T>(res: Response): Promise<T> {
  const text = await res.text()
  if (!res.ok) {
    throw new Error(text || `HTTP ${res.status}`)
  }
  return text ? (JSON.parse(text) as T) : ({} as T)
}

function shouldRefreshAfter401(path: string) {
  return !path.startsWith('/api/admin/me') && !path.startsWith('/api/admin/auth/')
}

function throwAuthExpired(message?: string): never {
  clearAuth()
  window.dispatchEvent(new CustomEvent(AUTH_EXPIRED_EVENT, { detail: { message } }))
  throw new AuthExpiredError(message)
}
