import { apiRequest } from './client'
import type { AuditEvent } from '../types/api'

export type AuditQuery = {
  kind?: string
  keyword?: string
  from?: string
  to?: string
  limit?: number
}

export function listAuditEvents(query: AuditQuery = {}) {
  const params = new URLSearchParams()
  if (query.kind) params.set('kind', query.kind)
  if (query.keyword) params.set('keyword', query.keyword)
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.limit) params.set('limit', String(query.limit))
  const search = params.toString()
  return apiRequest<AuditEvent[]>(`/api/admin/audit-events${search ? `?${search}` : ''}`)
}
