import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import EnvironmentProfileCard from '../EnvironmentProfileCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'admin.accounts.environmentProfile.poolStatus') {
        return `Profile pool: ${params?.count}/${params?.capacity} bound`
      }
      return key
    }
  })
}))

describe('EnvironmentProfileCard', () => {
  it('shows profile pool summary without slot details', () => {
    const wrapper = mount(EnvironmentProfileCard, {
      props: {
        family: 'claude',
        pool: {
          capacity: 2,
          slots: [
            {
              slot: 0,
              environment: 'linux',
              state: 'bound',
              profile: {
                family: 'code_cli',
                source: 'learned_verified_desktop',
                user_agent: 'claude-cli/2.1.0',
                client_id: 'client',
                device_id: 'device',
                session_seed: 'seed',
                platform: 'linux',
                arch: 'x64'
              }
            },
            {
              slot: 1,
              environment: 'windows',
              state: 'empty'
            }
          ]
        },
        singleEnvironment: true,
        locked: true,
        allowLearn: false,
        familyPreference: 'auto'
      }
    })

    expect(wrapper.text()).toContain('Profile pool: 1/2 bound')
    expect(wrapper.text()).not.toContain('Slot 1')
    expect(wrapper.text()).not.toContain('learned_verified_desktop')
    expect(wrapper.text()).not.toContain('code_cli ·')
  })
})
