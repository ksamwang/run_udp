import { apiRequest } from './client'
import type { AdminUser, HealthResponse } from '../types/api'

export function getHealth() {
  return apiRequest<HealthResponse>('/health')
}

export function getMe() {
  return apiRequest<{ user: AdminUser }>('/api/admin/me')
}
