import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import type { ClaudeOAuthPoolDetail } from '@/api/admin/claudeOAuthPools'
import ClaudeOAuthPoolsView from '../ClaudeOAuthPoolsView.vue'

const { list, get, setMode, showError, showSuccess } = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  setMode: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    claudeOAuthPools: {
      list,
      get,
      setMode,
      create: vi.fn(),
      update: vi.fn(),
      remove: vi.fn(),
      addCredential: vi.fn(),
      removeCredential: vi.fn(),
      resetCredentialBindings: vi.fn(),
      createCapsule: vi.fn(),
      activateCapsule: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function detail(qualified: boolean): ClaudeOAuthPoolDetail {
  return {
    pool: {
      id: 7,
      name: 'primary',
      provider: 'claude_oauth',
      status: 'active',
      mode: 'shadow',
      egress_route_id: 11,
      allowed_origins: ['https://api.anthropic.com/v1/messages'],
      allowed_models: ['claude-*'],
      active_capsule_set_version: 3,
      compatibility_digest: 'digest',
      session_ttl_seconds: 3600,
      created_at: '2026-07-12T00:00:00Z',
      updated_at: '2026-07-12T00:00:00Z',
    },
    credentials: [{ id: 1, pool_id: 7, account_id: 22, state: 'available' }],
    binding_counts: { '22': 4 },
    shadow_metrics: {
      pool_id: 7,
      requests: qualified ? 10000 : 9999,
      routing_diffs: 12,
      binding_errors: 0,
      capsule_invariant_failures: 0,
      direct_egress_attempts: 0,
      session_conflicts: 0,
      unapproved_business_diffs: 0,
      consecutive_days: 7,
      qualified,
    },
  }
}

async function mountView(poolDetail: ClaudeOAuthPoolDetail): Promise<VueWrapper> {
  list.mockResolvedValue([poolDetail.pool])
  get.mockResolvedValue(poolDetail)
  const wrapper = mount(ClaudeOAuthPoolsView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
  })
  await flushPromises()
  const poolButton = wrapper.findAll('button').find((button) => button.text().includes('primary'))
  expect(poolButton).toBeTruthy()
  await poolButton!.trigger('click')
  await flushPromises()
  return wrapper
}


describe('ClaudeOAuthPoolsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps enforcement disabled until backend shadow metrics qualify', async () => {
    const wrapper = await mountView(detail(false))
    const button = wrapper.findAll('button').find((candidate) => candidate.text() === 'admin.claudeOAuthPools.enforce')
    expect(button?.attributes('disabled')).toBeDefined()
  })

  it('allows a qualified pool to switch through the explicit mode endpoint', async () => {
    const wrapper = await mountView(detail(true))
    const button = wrapper.findAll('button').find((candidate) => candidate.text() === 'admin.claudeOAuthPools.enforce')
    expect(button?.attributes('disabled')).toBeUndefined()

    setMode.mockResolvedValue({ ...detail(true).pool, mode: 'enforce' })
    await button!.trigger('click')
    await flushPromises()

    expect(setMode).toHaveBeenCalledWith(7, 'enforce')
  })
})
