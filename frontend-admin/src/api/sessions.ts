import { apiRequest } from './client'
import type { Session } from '../types/api'

export function listSessions() {
  return apiRequest<Session[]>('/api/admin/sessions')
}
