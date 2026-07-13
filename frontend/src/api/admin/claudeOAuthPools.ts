import { apiClient } from '../client'

export type ClaudeOAuthPoolMode = 'shadow' | 'enforce'
export type ClaudeOAuthPoolStatus = 'active' | 'inactive'

export interface ClaudeOAuthPool {
  id: number
  name: string
  provider: 'claude_oauth'
  status: ClaudeOAuthPoolStatus
  mode: ClaudeOAuthPoolMode
  egress_route_id: number
  allowed_origins: string[]
  allowed_models: string[]
  active_capsule_set_version: number
  previous_capsule_set_version?: number
  compatibility_digest: string
  session_ttl_seconds: number
  shadow_started_at?: string
  shadow_qualified_at?: string
  created_at: string
  updated_at: string
}

export interface ClaudeOAuthPoolCredential {
  id: number
  pool_id: number
  account_id: number
  state: string
  cooldown_until?: string
}

export interface ClaudeOAuthShadowMetrics {
  pool_id: number
  requests: number
  routing_diffs: number
  binding_errors: number
  capsule_invariant_failures: number
  direct_egress_attempts: number
  session_conflicts: number
  unapproved_business_diffs: number
  consecutive_days: number
  started_at?: string
  last_observed_at?: string
  qualified: boolean
}

export interface ClaudeOAuthPoolDetail {
  pool: ClaudeOAuthPool
  credentials: ClaudeOAuthPoolCredential[]
  binding_counts: Record<string, number>
  shadow_metrics: ClaudeOAuthShadowMetrics
}

export interface ClaudeOAuthPoolInput {
  name: string
  status: ClaudeOAuthPoolStatus
  egress_route_id: number
  allowed_origins: string[]
  allowed_models: string[]
}

export async function list(): Promise<ClaudeOAuthPool[]> {
  const { data } = await apiClient.get<ClaudeOAuthPool[]>('/admin/claude-oauth-pools')
  return data
}

export async function get(id: number): Promise<ClaudeOAuthPoolDetail> {
  const { data } = await apiClient.get<ClaudeOAuthPoolDetail>(`/admin/claude-oauth-pools/${id}`)
  return data
}

export async function create(input: ClaudeOAuthPoolInput): Promise<ClaudeOAuthPool> {
  const { data } = await apiClient.post<ClaudeOAuthPool>('/admin/claude-oauth-pools', input)
  return data
}

export async function update(id: number, input: ClaudeOAuthPoolInput): Promise<ClaudeOAuthPool> {
  const { data } = await apiClient.put<ClaudeOAuthPool>(`/admin/claude-oauth-pools/${id}`, input)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/claude-oauth-pools/${id}`)
}

export async function addCredential(id: number, accountId: number): Promise<ClaudeOAuthPoolCredential> {
  const { data } = await apiClient.post<ClaudeOAuthPoolCredential>(`/admin/claude-oauth-pools/${id}/credentials`, {
    account_id: accountId
  })
  return data
}

export async function removeCredential(id: number, accountId: number): Promise<void> {
  await apiClient.delete(`/admin/claude-oauth-pools/${id}/credentials/${accountId}`)
}

export async function resetCredentialBindings(id: number, accountId: number): Promise<number> {
	const { data } = await apiClient.delete<{ deleted_bindings: number }>(
		`/admin/claude-oauth-pools/${id}/credentials/${accountId}/bindings`
	)
	return data.deleted_bindings
}

export async function setMode(id: number, mode: ClaudeOAuthPoolMode): Promise<ClaudeOAuthPool> {
  const { data } = await apiClient.post<ClaudeOAuthPool>(`/admin/claude-oauth-pools/${id}/mode`, { mode })
  return data
}

export default {
  list,
  get,
  create,
  update,
  remove,
  addCredential,
  removeCredential,
  resetCredentialBindings,
  setMode
}
