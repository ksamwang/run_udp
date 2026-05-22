import { apiRequest } from './client'
import type { TunnelState } from '../types/api'

export function listTunnelStates() {
  return apiRequest<TunnelState[]>('/api/tunnel-states')
}
