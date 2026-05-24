import { apiRequest } from './client'
import type { VirtualACLRule, VirtualACLRulePayload, VirtualAddress, VirtualNetwork, VirtualPeerState, VirtualRoute, VirtualRoutePayload } from '../types/api'

export function listVirtualNetworks() {
  return apiRequest<VirtualNetwork[]>('/api/admin/lan/networks')
}

export function updateVirtualNetwork(id: number, payload: Pick<VirtualNetwork, 'name' | 'cidr' | 'enabled'>) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/networks/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function listVirtualAddresses(networkID?: number) {
  const query = networkID ? `?network_id=${networkID}` : ''
  return apiRequest<VirtualAddress[]>(`/api/admin/lan/addresses${query}`)
}

export function updateVirtualAddress(deviceID: string, payload: Omit<VirtualAddress, 'device_id' | 'created_at' | 'updated_at'>) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/addresses/${encodeURIComponent(deviceID)}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function listVirtualACLRules(networkID?: number) {
  const query = networkID ? `?network_id=${networkID}` : ''
  return apiRequest<VirtualACLRule[]>(`/api/admin/lan/acl${query}`)
}

export function createVirtualACLRule(payload: VirtualACLRulePayload) {
  return apiRequest<VirtualACLRule>('/api/admin/lan/acl', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateVirtualACLRule(id: number, payload: VirtualACLRulePayload) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/acl/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function deleteVirtualACLRule(id: number) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/acl/${id}`, {
    method: 'DELETE',
  })
}

export function listVirtualRoutes(networkID?: number, deviceID?: string) {
  const params = new URLSearchParams()
  if (networkID) params.set('network_id', String(networkID))
  if (deviceID) params.set('device_id', deviceID)
  const query = params.toString() ? `?${params.toString()}` : ''
  return apiRequest<VirtualRoute[]>(`/api/admin/lan/routes${query}`)
}

export function createVirtualRoute(payload: VirtualRoutePayload) {
  return apiRequest<VirtualRoute>('/api/admin/lan/routes', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateVirtualRoute(id: number, payload: VirtualRoutePayload) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/routes/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function deleteVirtualRoute(id: number) {
  return apiRequest<{ ok: boolean }>(`/api/admin/lan/routes/${id}`, {
    method: 'DELETE',
  })
}

export function listVirtualPeerStates(networkID?: number) {
  const query = networkID ? `?network_id=${networkID}` : ''
  return apiRequest<VirtualPeerState[]>(`/api/admin/lan/peer-states${query}`)
}
