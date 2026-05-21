import { apiRequest } from './client'
import type { ForwardRule } from '../types/api'

export function listRules() {
  return apiRequest<ForwardRule[]>('/api/forwards')
}
