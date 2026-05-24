import { apiRequest } from './client'
import type { ReleaseUploadResponse, Settings } from '../types/api'

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

export function uploadReleasePackage(product: 'client' | 'lan', file: File) {
  const data = new FormData()
  data.set('file', file)
  return apiRequest<ReleaseUploadResponse>(`/api/admin/releases/${product}/upload`, {
    method: 'POST',
    body: data,
  })
}

export function validateReleaseURL(url: string) {
  return apiRequest<{ ok: boolean }>('/api/admin/releases/validate-url', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}
