<template>
  <section
    class="rounded-lg border border-gray-200 bg-gray-50/60 p-4 dark:border-dark-600 dark:bg-dark-800/40"
  >
    <div class="mb-4 flex items-start justify-between gap-3">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {{ title }}
        </h3>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
          {{ description }}
        </p>
      </div>
      <button
        v-if="!createMode"
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="(!profile && !hasPoolSlots) || resetting"
        @click="emit('reset')"
      >
        {{
          resetting
            ? t('admin.accounts.environmentProfile.resetting')
            : t('admin.accounts.environmentProfile.reset')
        }}
      </button>
    </div>

    <div class="space-y-3">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="input-label mb-0">{{
            t('admin.accounts.environmentProfile.singleEnvironment')
          }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.environmentProfile.singleEnvironmentDesc') }}
          </p>
        </div>
        <button
          type="button"
          :class="toggleClass(singleEnvironment)"
          @click="singleEnvironmentModel = !singleEnvironmentModel"
        >
          <span :class="toggleKnobClass(singleEnvironment)" />
        </button>
      </div>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{
            t('admin.accounts.environmentProfile.familyPreference')
          }}</label>
          <select v-model="familyPreferenceModel" class="input">
            <option v-for="option in familyOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.environmentProfile.locked') }}</label>
          <button
            type="button"
            class="mt-1 flex w-full items-center justify-between rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-dark-500 dark:bg-dark-700"
            @click="lockedModel = !lockedModel"
          >
            <span class="text-gray-700 dark:text-gray-200">
              {{ locked ? t('common.enabled') : t('common.disabled') }}
            </span>
            <span :class="toggleClass(locked)">
              <span :class="toggleKnobClass(locked)" />
            </span>
          </button>
        </div>
      </div>

      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="input-label mb-0">{{ allowLearnLabel }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ allowLearnDescription }}
          </p>
        </div>
        <button
          type="button"
          :class="toggleClass(allowLearn)"
          @click="allowLearnModel = !allowLearnModel"
        >
          <span :class="toggleKnobClass(allowLearn)" />
        </button>
      </div>
    </div>

    <div
      v-if="hasPoolSlots"
      class="mt-4 rounded-md border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-700"
    >
      <div
        class="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400"
      >
        {{ t('admin.accounts.environmentProfile.poolStatus', { count: boundSlotCount, capacity: poolCapacity }) }}
      </div>

      <!-- v2 按槽位编辑表单 -->
      <div v-if="isV2Pool" class="space-y-3">
        <div
          v-for="slot in editableSlots"
          :key="slot.environment"
          class="rounded-md border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-800/40"
        >
          <div class="mb-2 flex items-center justify-between">
            <span class="text-xs font-semibold uppercase tracking-wide text-gray-700 dark:text-gray-200">
              {{ slotLabel(slot.environment) }}
            </span>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="savingSlot === slot.environment || !slot.profile"
              @click="toggleEditSlot(slot.environment)"
            >
              {{ editingSlot === slot.environment ? t('common.cancel') : t('admin.accounts.environmentProfile.editSlot') }}
            </button>
          </div>

          <div v-if="slot.profile" class="space-y-2">
            <div v-for="field in editableFields" :key="field.key">
              <label class="input-label mb-0.5 text-xs">{{ field.label }}</label>
              <input
                v-if="editingSlot === slot.environment"
                v-model="draft[slot.environment][field.key]"
                type="text"
                class="input input-sm"
                :placeholder="slotFieldValue(slot, field.key)"
              />
              <p
                v-else
                class="truncate text-xs font-medium text-gray-900 dark:text-gray-100"
                :title="slotFieldValue(slot, field.key)"
              >
                {{ slotFieldValue(slot, field.key) || '-' }}
              </p>
            </div>
            <div v-if="editingSlot === slot.environment" class="flex justify-end gap-2 pt-1">
              <button
                type="button"
                class="btn btn-primary btn-sm"
                :disabled="savingSlot === slot.environment"
                @click="saveSlot(slot.environment)"
              >
                {{ savingSlot === slot.environment ? t('common.saving') : t('common.save') }}
              </button>
            </div>
          </div>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.environmentProfile.noProfile') }}
          </p>
        </div>
      </div>
    </div>

    <div
      v-else-if="profile"
      class="mt-4 rounded-md border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-700"
    >
      <div
        class="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400"
      >
        {{ t('admin.accounts.environmentProfile.status') }}
      </div>
      <dl class="grid grid-cols-1 gap-2 text-xs sm:grid-cols-2">
        <div v-for="row in profileRows" :key="row.label" class="min-w-0">
          <dt class="text-gray-500 dark:text-gray-400">{{ row.label }}</dt>
          <dd class="truncate font-medium text-gray-900 dark:text-gray-100" :title="row.value">
            {{ row.value }}
          </dd>
        </div>
      </dl>
    </div>
    <p
      v-else
      class="mt-4 rounded-md bg-white px-3 py-2 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-400"
    >
      {{
        createMode
          ? t('admin.accounts.environmentProfile.createPending')
          : t('admin.accounts.environmentProfile.noProfile')
      }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ClaudeEnvironmentProfile, ClaudeEnvironmentProfilePool, ClaudeEnvironmentProfileSlot, CodexEnvironmentProfile, CodexEnvironmentProfilePool, CodexEnvironmentProfileSlot } from '@/types'

type ProfileFamily = 'claude' | 'codex'

interface Option {
  value: string
  label: string
}

interface ProfileRow {
  label: string
  value: string
}

const props = withDefaults(
  defineProps<{
    family: ProfileFamily
    profile?: ClaudeEnvironmentProfile | CodexEnvironmentProfile
    pool?: ClaudeEnvironmentProfilePool | CodexEnvironmentProfilePool
    singleEnvironment: boolean
    locked: boolean
    allowLearn: boolean
    familyPreference: string
    resetting?: boolean
    createMode?: boolean
    savingSlot?: string | null
  }>(),
  {
    profile: undefined,
    pool: undefined,
    resetting: false,
    createMode: false,
    savingSlot: null
  }
)

const emit = defineEmits<{
  'update:singleEnvironment': [value: boolean]
  'update:locked': [value: boolean]
  'update:allowLearn': [value: boolean]
  'update:familyPreference': [value: string]
  reset: []
  'save-slot': [slot: string, profile: Record<string, unknown>]
}>()

const { t } = useI18n()

const singleEnvironmentModel = computed({
  get: () => props.singleEnvironment,
  set: (value: boolean) => emit('update:singleEnvironment', value)
})

const lockedModel = computed({
  get: () => props.locked,
  set: (value: boolean) => emit('update:locked', value)
})

const allowLearnModel = computed({
  get: () => props.allowLearn,
  set: (value: boolean) => emit('update:allowLearn', value)
})

const familyPreferenceModel = computed({
  get: () => props.familyPreference,
  set: (value: string) => emit('update:familyPreference', value)
})

const title = computed(() =>
  props.family === 'claude'
    ? t('admin.accounts.environmentProfile.claudeTitle')
    : t('admin.accounts.environmentProfile.codexTitle')
)

const description = computed(() =>
  props.family === 'claude'
    ? t('admin.accounts.environmentProfile.claudeDesc')
    : t('admin.accounts.environmentProfile.codexDesc')
)

const allowLearnLabel = computed(() =>
  props.family === 'claude'
    ? t('admin.accounts.environmentProfile.allowDesktopLearn')
    : t('admin.accounts.environmentProfile.allowOfficialClientLearn')
)

const allowLearnDescription = computed(() =>
  props.family === 'claude'
    ? t('admin.accounts.environmentProfile.allowDesktopLearnDesc')
    : t('admin.accounts.environmentProfile.allowOfficialClientLearnDesc')
)

const familyOptions = computed<Option[]>(() => {
  if (props.family === 'claude') {
    return [
      {
        value: 'auto',
        label: t('admin.accounts.environmentProfile.familyAuto')
      },
      {
        value: 'code_cli',
        label: t('admin.accounts.environmentProfile.claudeCodeCLI')
      },
      {
        value: 'desktop',
        label: t('admin.accounts.environmentProfile.claudeDesktop')
      }
    ]
  }
  return [
    { value: 'auto', label: t('admin.accounts.environmentProfile.familyAuto') },
    { value: 'cli', label: t('admin.accounts.environmentProfile.codexCLI') },
    {
      value: 'desktop',
      label: t('admin.accounts.environmentProfile.codexDesktop')
    },
    {
      value: 'vscode',
      label: t('admin.accounts.environmentProfile.codexVSCode')
    }
  ]
})

function stringifyValue(value: unknown): string {
  if (typeof value === 'string' && value.trim()) return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '-'
}

const poolSlots = computed(() => (Array.isArray(props.pool?.slots) ? props.pool.slots : []))
const hasPoolSlots = computed(() => poolSlots.value.length > 0)
const poolCapacity = computed(() => props.pool?.capacity ?? poolSlots.value.length)
const boundSlotCount = computed(() => poolSlots.value.filter((slot) => slot.state === 'bound').length)
const isV2Pool = computed(() => (props.pool?.schema ?? '') === 'v2')
const editableSlots = computed(() =>
  poolSlots.value.filter((s) => s.environment !== 'desktop')
)

interface EditField {
  key: string
  label: string
}

const editableFields = computed<EditField[]>(() => {
  if (props.family === 'claude') {
    return [
      { key: 'device_id', label: t('admin.accounts.environmentProfile.deviceId') },
      { key: 'client_id', label: t('admin.accounts.environmentProfile.clientId') },
      { key: 'user_agent', label: t('admin.accounts.environmentProfile.userAgent') },
      { key: 'client_version', label: t('admin.accounts.environmentProfile.clientVersion') },
      { key: 'platform', label: t('admin.accounts.environmentProfile.platform') },
      { key: 'arch', label: t('admin.accounts.environmentProfile.arch') }
    ]
  }
  return [
    { key: 'user_agent', label: t('admin.accounts.environmentProfile.userAgent') },
    { key: 'originator', label: t('admin.accounts.environmentProfile.originator') },
    { key: 'version', label: t('admin.accounts.environmentProfile.version') },
    { key: 'session_seed', label: t('admin.accounts.environmentProfile.sessionSeed') },
    { key: 'conversation_seed', label: t('admin.accounts.environmentProfile.conversationSeed') },
    { key: 'tls_profile', label: t('admin.accounts.environmentProfile.tlsProfile') },
    { key: 'platform', label: t('admin.accounts.environmentProfile.platform') },
    { key: 'arch', label: t('admin.accounts.environmentProfile.arch') }
  ]
})

const editingSlot = ref<string | null>(null)
const draft = reactive<Record<string, Record<string, string>>>({})

function slotLabel(env: string): string {
  const map: Record<string, string> = {
    windows: t('admin.accounts.environmentProfile.slotWindows'),
    macos: t('admin.accounts.environmentProfile.slotMacos'),
    linux: t('admin.accounts.environmentProfile.slotLinux')
  }
  return map[env] ?? env
}

function ensureDraft(env: string) {
  if (!draft[env]) {
    draft[env] = {}
  }
}

function slotFieldValue(
  slot: ClaudeEnvironmentProfileSlot | CodexEnvironmentProfileSlot,
  key: string
): string {
  const profile = slot.profile as Record<string, unknown> | null | undefined
  if (!profile) return ''
  const v = profile[key]
  if (typeof v === 'string') return v
  if (v == null) return ''
  return String(v)
}

function toggleEditSlot(env: string) {
  if (editingSlot.value === env) {
    editingSlot.value = null
    return
  }
  const slot = editableSlots.value.find((s) => s.environment === env)
  ensureDraft(env)
  for (const field of editableFields.value) {
    draft[env][field.key] = slot ? slotFieldValue(slot, field.key) : ''
  }
  editingSlot.value = env
}

function saveSlot(env: string) {
  const payload: Record<string, unknown> = {}
  for (const field of editableFields.value) {
    const v = (draft[env]?.[field.key] ?? '').trim()
    if (v !== '') {
      payload[field.key] = v
    }
  }
  emit('save-slot', env, payload)
  editingSlot.value = null
}
const profileRows = computed<ProfileRow[]>(() => {
  const profile = props.profile
  if (!profile) return []

  const rows: ProfileRow[] = [
    {
      label: t('admin.accounts.environmentProfile.family'),
      value: stringifyValue(profile.family)
    },
    {
      label: t('admin.accounts.environmentProfile.source'),
      value: stringifyValue(profile.source)
    },
    {
      label: t('admin.accounts.environmentProfile.platformArch'),
      value: [profile.platform, profile.arch].filter(Boolean).join(' / ') || '-'
    }
  ]

  if (props.family === 'claude') {
    const claudeProfile = profile as ClaudeEnvironmentProfile
    rows.push(
      {
        label: t('admin.accounts.environmentProfile.clientVersion'),
        value: stringifyValue(claudeProfile.client_version)
      },
      {
        label: t('admin.accounts.environmentProfile.userAgent'),
        value: stringifyValue(claudeProfile.user_agent)
      },
      {
        label: t('admin.accounts.environmentProfile.telemetryPolicy'),
        value: stringifyValue(claudeProfile.telemetry_policy)
      },
      {
        label: t('admin.accounts.environmentProfile.updatedAt'),
        value: stringifyValue(claudeProfile.updated_at)
      }
    )
    return rows
  }

  const codexProfile = profile as CodexEnvironmentProfile
  rows.push(
    {
      label: t('admin.accounts.environmentProfile.originator'),
      value: stringifyValue(codexProfile.originator)
    },
    {
      label: t('admin.accounts.environmentProfile.version'),
      value: stringifyValue(codexProfile.version)
    },
    {
      label: t('admin.accounts.environmentProfile.tlsProfile'),
      value: stringifyValue(codexProfile.tls_profile)
    },
    {
      label: t('admin.accounts.environmentProfile.updatedAt'),
      value: stringifyValue(codexProfile.updated_at)
    }
  )
  return rows
})

function toggleClass(enabled: boolean): string[] {
  return [
    'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
    enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
  ]
}

function toggleKnobClass(enabled: boolean): string[] {
  return [
    'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
    enabled ? 'translate-x-5' : 'translate-x-0'
  ]
}
</script>
