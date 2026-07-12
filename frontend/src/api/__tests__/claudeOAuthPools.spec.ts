import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, delete: del },
}))

import { createCapsule, get as getPool, resetCredentialBindings, setMode } from '@/api/admin/claudeOAuthPools'
import type { ClaudeOAuthPoolDetail } from '@/api/admin/claudeOAuthPools'

describe('claude oauth pool admin api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
		del.mockReset()
  })

  it('loads the pool detail envelope from the dedicated endpoint', async () => {
    const detail = { pool: { id: 7 }, credentials: [], shadow_metrics: { qualified: false } } as ClaudeOAuthPoolDetail
    get.mockResolvedValue({ data: detail })

    await expect(getPool(7)).resolves.toBe(detail)
    expect(get).toHaveBeenCalledWith('/admin/claude-oauth-pools/7')
  })

  it('resets active credential bindings through the scoped pool endpoint', async () => {
    del.mockResolvedValue({ data: { deleted_bindings: 2 } })

    await expect(resetCredentialBindings(7, 12)).resolves.toBe(2)
    expect(del).toHaveBeenCalledWith('/admin/claude-oauth-pools/7/credentials/12/bindings')
  })

  it('creates immutable capsule versions and switches mode through explicit endpoints', async () => {
    post.mockResolvedValue({ data: { id: 7, mode: 'enforce' } })

    await createCapsule(7, { version: 3, cli_version: '1.0.42', profile_timezone: 'UTC' })
    await setMode(7, 'enforce')

    expect(post).toHaveBeenNthCalledWith(1, '/admin/claude-oauth-pools/7/capsules', {
      version: 3,
      cli_version: '1.0.42',
      profile_timezone: 'UTC',
    })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/claude-oauth-pools/7/mode', { mode: 'enforce' })
  })
})
