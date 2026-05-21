import { apiRequest } from './client'
import type { Device } from '../types/api'

export function listDevices() {
  return apiRequest<Device[]>('/api/devices')
}
