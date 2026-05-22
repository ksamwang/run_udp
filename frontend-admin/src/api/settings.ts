import { apiRequest } from './client'
import type { Settings } from '../types/api'

export function getSettings() {
  return apiRequest<Settings>('/api/admin/settings')
}

export function updateSettings(payload: Settings) {
  return apiRequest<{ ok: boolean }>('/api/admin/settings', {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function changePassword(payload: { current_password: string; new_password: string }) {
  return apiRequest<{ ok: boolean }>('/api/admin/password', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
