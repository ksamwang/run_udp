import { apiRequest } from './client'
import type { HealthResponse } from '../types/api'

export function getHealth() {
  return apiRequest<HealthResponse>('/health')
}

export function getMe() {
  return apiRequest<{ user: { id: string; username: string; name: string; role: string } }>('/api/admin/me')
}
