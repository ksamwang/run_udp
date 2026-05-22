import { apiRequest } from './client'
import type { Device } from '../types/api'

export function listDevices() {
  return apiRequest<Device[]>('/api/admin/devices')
}

export function setDeviceEnabled(id: string, enabled: boolean) {
  return apiRequest<{ ok: boolean }>(`/api/admin/devices/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}

export function deleteDevice(id: string) {
  return apiRequest<{ ok: boolean }>(`/api/admin/devices/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}
