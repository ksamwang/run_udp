import { apiRequest } from './client'
import type { ForwardRule, ForwardRulePayload } from '../types/api'

export function listRules() {
  return apiRequest<ForwardRule[]>('/api/admin/rules')
}

export function createRule(payload: ForwardRulePayload) {
  return apiRequest<ForwardRule>('/api/admin/rules', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateRule(id: number, payload: ForwardRulePayload) {
  return apiRequest<{ ok: boolean }>(`/api/admin/rules/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function deleteRule(id: number) {
  return apiRequest<{ ok: boolean }>(`/api/admin/rules/${id}`, {
    method: 'DELETE',
  })
}

export function setRuleEnabled(rule: ForwardRule, enabled: boolean) {
  return updateRule(rule.id, {
    name: rule.name,
    source_id: rule.source_id,
    target_id: rule.target_id,
    profile: rule.profile || 'interactive',
    local_port: rule.local_port,
    target_host: rule.target_host,
    target_port: rule.target_port,
    enabled,
  })
}
