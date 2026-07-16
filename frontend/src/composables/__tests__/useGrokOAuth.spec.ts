import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const messages: Record<string, string> = {
        'admin.accounts.oauth.grok.failedToExchangeCode': 'Grok 授权码兑换失败',
        'admin.accounts.oauth.grok.errors.GROK_OAUTH_INVALID_STATE':
          'Grok OAuth state 与当前会话不匹配。请粘贴同一次生成的授权链接返回的回调 URL。'
      }
      return messages[key] ?? key
    }
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    grok: {
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      refreshGrokToken: vi.fn()
    }
  }
}))

import { useGrokOAuth, GROK_CLI_BASE_URL, GROK_CLI_HEADERS, GROK_OAUTH_TOKEN_ENDPOINT } from '@/composables/useGrokOAuth'
import { adminAPI } from '@/api/admin'

describe('useGrokOAuth.exchangeAuthCode', () => {
  it('shows a state mismatch recovery hint from structured backend errors', async () => {
    vi.mocked(adminAPI.grok.exchangeCode).mockRejectedValueOnce({
      status: 400,
      reason: 'GROK_OAUTH_INVALID_STATE',
      message: 'invalid oauth state'
    })
    const oauth = useGrokOAuth()

    const tokenInfo = await oauth.exchangeAuthCode({
      code: 'code',
      sessionId: 'session-id',
      state: 'wrong-state'
    })

    expect(tokenInfo).toBeNull()
    expect(oauth.error.value).toBe(
      'Grok OAuth state 与当前会话不匹配。请粘贴同一次生成的授权链接返回的回调 URL。'
    )
  })
})

describe('useGrokOAuth.buildCredentials', () => {
  it(' assembles CLI proxy base URL and identity headers for OAuth tokens', () => {
    const oauth = useGrokOAuth()
    const creds = oauth.buildCredentials({
      access_token: 'at-123',
      refresh_token: 'rt-456',
      token_type: 'Bearer',
      expires_at: 1784233642,
      expires_in: 21600,
      client_id: 'client-1',
      email: 'user@example.com',
      subscription_tier: 'free'
    })

    expect(creds.base_url).toBe(GROK_CLI_BASE_URL)
    expect(creds.token_endpoint).toBe(GROK_OAUTH_TOKEN_ENDPOINT)
    expect(creds.auth_kind).toBe('oauth')
    expect(creds.access_token).toBe('at-123')
    expect(creds.refresh_token).toBe('rt-456')
    expect(creds.expires_at).toBe(new Date(1784233642 * 1000).toISOString())
    expect(creds.headers).toEqual({ ...GROK_CLI_HEADERS })
    // Must never default OAuth credentials to the public API host.
    expect(String(creds.base_url)).not.toContain('api.x.ai')
  })
})
