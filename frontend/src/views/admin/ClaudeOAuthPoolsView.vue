<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6 p-4 sm:p-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.claudeOAuthPools.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.claudeOAuthPools.description') }}</p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="loadPools">{{ t('common.refresh') }}</button>
          <button class="btn btn-primary" @click="openCreate">{{ t('admin.claudeOAuthPools.createPool') }}</button>
        </div>
      </div>

      <div v-if="loading" class="card p-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="pools.length === 0" class="card p-8 text-center text-sm text-gray-500">
        {{ t('admin.claudeOAuthPools.empty') }}
      </div>
      <div v-else class="grid gap-4 lg:grid-cols-2">
        <button
          v-for="pool in pools"
          :key="pool.id"
          type="button"
          class="card p-5 text-left transition hover:border-primary-400"
          :class="selectedId === pool.id ? 'border-primary-500 ring-1 ring-primary-500' : ''"
          @click="selectPool(pool.id)"
        >
          <div class="flex items-start justify-between gap-4">
            <div>
              <div class="font-semibold text-gray-900 dark:text-white">{{ pool.name }}</div>
              <div class="mt-1 text-xs text-gray-500">#{{ pool.id }} · egress #{{ pool.egress_route_id }}</div>
            </div>
            <span :class="pool.mode === 'enforce' ? 'badge badge-success' : 'badge badge-warning'">
              {{ pool.mode }}
            </span>
          </div>
          <div class="mt-4 grid grid-cols-3 gap-3 text-xs">
            <div><div class="text-gray-500">{{ t('admin.claudeOAuthPools.status') }}</div><div class="mt-1 font-medium">{{ pool.status }}</div></div>
            <div><div class="text-gray-500">{{ t('admin.claudeOAuthPools.capsule') }}</div><div class="mt-1 font-medium">v{{ pool.active_capsule_set_version || '-' }}</div></div>
            <div><div class="text-gray-500">TTL</div><div class="mt-1 font-medium">{{ pool.session_ttl_seconds }}s</div></div>
          </div>
        </button>
      </div>

      <div v-if="detail" class="grid gap-6 xl:grid-cols-3">
        <section class="card space-y-4 p-5 xl:col-span-2">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ detail.pool.name }}</h2>
            <div class="flex gap-2">
              <button class="btn btn-secondary" @click="openEdit">{{ t('common.edit') }}</button>
              <button class="btn btn-danger" @click="deleteSelected">{{ t('common.delete') }}</button>
            </div>
          </div>

          <div class="grid gap-3 sm:grid-cols-2">
            <div class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">
              <div class="text-gray-500">{{ t('admin.claudeOAuthPools.models') }}</div>
              <div class="mt-1 break-words font-mono text-xs">{{ detail.pool.allowed_models.join(', ') }}</div>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800">
              <div class="text-gray-500">{{ t('admin.claudeOAuthPools.origins') }}</div>
              <div class="mt-1 break-words font-mono text-xs">{{ detail.pool.allowed_origins.join(', ') }}</div>
            </div>
          </div>

          <div>
            <div class="mb-2 flex items-center justify-between">
              <h3 class="font-medium text-gray-900 dark:text-white">{{ t('admin.claudeOAuthPools.credentials') }}</h3>
              <form class="flex gap-2" @submit.prevent="addCredential">
                <input v-model.number="credentialAccountId" class="input w-36" type="number" min="1" :placeholder="t('admin.claudeOAuthPools.accountId')" required />
                <button class="btn btn-secondary" :disabled="saving">{{ t('common.add') }}</button>
              </form>
            </div>
            <div v-if="detail.credentials.length === 0" class="rounded-lg border border-dashed p-4 text-center text-sm text-gray-500">
              {{ t('admin.claudeOAuthPools.noCredentials') }}
            </div>
            <div v-else class="space-y-2">
              <div v-for="credential in detail.credentials" :key="credential.id" class="flex items-center justify-between rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
                <div>
                  <div>#{{ credential.account_id }} · {{ credential.state }}</div>
                  <div class="text-xs text-gray-500">{{ t('admin.claudeOAuthPools.bindingCount') }}: {{ detail.binding_counts[String(credential.account_id)] || 0 }}</div>
                </div>
                <div class="flex gap-3">
                  <button class="text-amber-600 hover:underline" @click="resetBindings(credential.account_id)">{{ t('admin.claudeOAuthPools.resetBindings') }}</button>
                  <button class="text-red-600 hover:underline" @click="removeCredential(credential.account_id)">{{ t('admin.claudeOAuthPools.remove') }}</button>
                </div>
              </div>
            </div>
          </div>

          <div class="border-t border-gray-200 pt-4 dark:border-dark-700">
            <h3 class="mb-3 font-medium text-gray-900 dark:text-white">{{ t('admin.claudeOAuthPools.capsuleManagement') }}</h3>
            <form class="grid gap-3 sm:grid-cols-4" @submit.prevent="createAndActivateCapsule">
              <input v-model.number="capsule.version" class="input" type="number" min="1" :placeholder="t('admin.claudeOAuthPools.version')" required />
              <input v-model="capsule.cli_version" class="input" type="text" placeholder="Claude CLI version" required />
              <input v-model="capsule.profile_timezone" class="input" type="text" placeholder="UTC" />
              <button class="btn btn-primary" :disabled="saving">{{ t('admin.claudeOAuthPools.createActivate') }}</button>
            </form>
          </div>
        </section>

        <aside class="card space-y-4 p-5">
          <div class="flex items-center justify-between">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Shadow gate</h2>
            <span :class="detail.shadow_metrics.qualified ? 'badge badge-success' : 'badge badge-warning'">
              {{ detail.shadow_metrics.qualified ? t('admin.claudeOAuthPools.qualified') : t('admin.claudeOAuthPools.collecting') }}
            </span>
          </div>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <MetricCard :label="t('admin.claudeOAuthPools.requests')" :value="`${detail.shadow_metrics.requests} / 10000`" />
            <MetricCard :label="t('admin.claudeOAuthPools.days')" :value="`${detail.shadow_metrics.consecutive_days} / 7`" />
            <MetricCard :label="t('admin.claudeOAuthPools.routingDiffs')" :value="detail.shadow_metrics.routing_diffs" />
            <MetricCard :label="t('admin.claudeOAuthPools.hardFailures')" :value="hardFailures" :danger="hardFailures > 0" />
          </div>
          <div class="space-y-1 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
            <div>binding: {{ detail.shadow_metrics.binding_errors }}</div>
            <div>capsule: {{ detail.shadow_metrics.capsule_invariant_failures }}</div>
            <div>egress: {{ detail.shadow_metrics.direct_egress_attempts }}</div>
            <div>session: {{ detail.shadow_metrics.session_conflicts }}</div>
            <div>business: {{ detail.shadow_metrics.unapproved_business_diffs }}</div>
          </div>
          <button
            v-if="detail.pool.mode === 'shadow'"
            class="btn btn-primary w-full"
            :disabled="!detail.shadow_metrics.qualified || saving"
            @click="setMode('enforce')"
          >
            {{ t('admin.claudeOAuthPools.enforce') }}
          </button>
          <button v-else class="btn btn-warning w-full" :disabled="saving" @click="setMode('shadow')">
            {{ t('admin.claudeOAuthPools.backToShadow') }}
          </button>
        </aside>
      </div>
    </div>

    <div v-if="showForm" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="showForm = false">
      <form class="card w-full max-w-xl space-y-4 p-6" @submit.prevent="savePool">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ editingId ? t('common.edit') : t('admin.claudeOAuthPools.createPool') }}</h2>
        <input v-model="form.name" class="input" type="text" :placeholder="t('admin.claudeOAuthPools.name')" required />
        <div class="grid gap-3 sm:grid-cols-2">
          <input v-model.number="form.egress_route_id" class="input" type="number" min="1" :placeholder="t('admin.claudeOAuthPools.egressRoute')" required />
          <select v-model="form.status" class="input"><option value="active">active</option><option value="inactive">inactive</option></select>
        </div>
        <textarea v-model="modelsText" class="input min-h-24" :placeholder="t('admin.claudeOAuthPools.modelsHint')" required />
        <textarea v-model="originsText" class="input min-h-24" :placeholder="t('admin.claudeOAuthPools.originsHint')" required />
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" @click="showForm = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving">{{ t('common.save') }}</button>
        </div>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { adminAPI } from '@/api/admin'
import type { ClaudeOAuthCapsuleInput, ClaudeOAuthPool, ClaudeOAuthPoolDetail, ClaudeOAuthPoolInput, ClaudeOAuthPoolMode } from '@/api/admin'
import { useAppStore } from '@/stores/app'

const MetricCard = defineComponent({
  props: { label: { type: String, required: true }, value: { type: [String, Number], required: true }, danger: Boolean },
  setup(props) {
    return () => h('div', { class: 'rounded-lg border border-gray-200 p-3 dark:border-dark-700' }, [
      h('div', { class: 'text-xs text-gray-500' }, props.label),
      h('div', { class: ['mt-1 font-semibold', props.danger ? 'text-red-600' : 'text-gray-900 dark:text-white'] }, String(props.value))
    ])
  }
})

const { t } = useI18n()
const appStore = useAppStore()
const pools = ref<ClaudeOAuthPool[]>([])
const detail = ref<ClaudeOAuthPoolDetail | null>(null)
const selectedId = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const showForm = ref(false)
const editingId = ref<number | null>(null)
const credentialAccountId = ref<number | null>(null)
const modelsText = ref('claude-*')
const originsText = ref('https://api.anthropic.com/v1/messages\nhttps://api.anthropic.com/v1/messages/count_tokens')
const form = reactive<ClaudeOAuthPoolInput>({ name: '', status: 'active', egress_route_id: 0, allowed_origins: [], allowed_models: [] })
const capsule = reactive<ClaudeOAuthCapsuleInput>({ version: 1, cli_version: '', profile_timezone: 'UTC' })
const hardFailures = computed(() => {
  const metrics = detail.value?.shadow_metrics
  return metrics ? metrics.binding_errors + metrics.capsule_invariant_failures + metrics.direct_egress_attempts + metrics.session_conflicts + metrics.unapproved_business_diffs : 0
})

function lines(value: string): string[] {
  return value.split(/[\n,]/).map(item => item.trim()).filter(Boolean)
}

async function loadPools() {
  loading.value = true
  try {
    pools.value = await adminAPI.claudeOAuthPools.list()
    if (selectedId.value && pools.value.some(pool => pool.id === selectedId.value)) await selectPool(selectedId.value)
  } catch (error: any) {
    appStore.showError(error?.message || t('common.unknownError'))
  } finally {
    loading.value = false
  }
}

async function selectPool(id: number) {
  selectedId.value = id
  try {
    detail.value = await adminAPI.claudeOAuthPools.get(id)
    capsule.version = Math.max(1, detail.value.pool.active_capsule_set_version + 1)
  } catch (error: any) {
    appStore.showError(error?.message || t('common.unknownError'))
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { name: '', status: 'active', egress_route_id: 0, allowed_origins: [], allowed_models: [] })
  modelsText.value = 'claude-*'
  originsText.value = 'https://api.anthropic.com/v1/messages\nhttps://api.anthropic.com/v1/messages/count_tokens'
  showForm.value = true
}

function openEdit() {
  if (!detail.value) return
  editingId.value = detail.value.pool.id
  Object.assign(form, {
    name: detail.value.pool.name,
    status: detail.value.pool.status,
    egress_route_id: detail.value.pool.egress_route_id,
    allowed_origins: [...detail.value.pool.allowed_origins],
    allowed_models: [...detail.value.pool.allowed_models]
  })
  modelsText.value = detail.value.pool.allowed_models.join('\n')
  originsText.value = detail.value.pool.allowed_origins.join('\n')
  showForm.value = true
}

async function savePool() {
  saving.value = true
  try {
    const input = { ...form, allowed_models: lines(modelsText.value), allowed_origins: lines(originsText.value) }
    const pool = editingId.value
      ? await adminAPI.claudeOAuthPools.update(editingId.value, input)
      : await adminAPI.claudeOAuthPools.create(input)
    showForm.value = false
    selectedId.value = pool.id
    await loadPools()
    appStore.showSuccess(t('common.saved'))
  } catch (error: any) {
    appStore.showError(error?.message || t('common.unknownError'))
  } finally {
    saving.value = false
  }
}

async function deleteSelected() {
  if (!detail.value || !window.confirm(t('admin.claudeOAuthPools.deleteConfirm'))) return
  await runAction(async () => {
    await adminAPI.claudeOAuthPools.remove(detail.value!.pool.id)
    selectedId.value = null
    detail.value = null
    await loadPools()
  })
}

async function addCredential() {
  if (!detail.value || !credentialAccountId.value) return
  await runAction(async () => {
    await adminAPI.claudeOAuthPools.addCredential(detail.value!.pool.id, credentialAccountId.value!)
    credentialAccountId.value = null
    await selectPool(detail.value!.pool.id)
  })
}

async function removeCredential(accountId: number) {
  if (!detail.value) return
  await runAction(async () => {
    await adminAPI.claudeOAuthPools.removeCredential(detail.value!.pool.id, accountId)
    await selectPool(detail.value!.pool.id)
  })
}

async function resetBindings(accountId: number) {
  if (!detail.value || !window.confirm(t('admin.claudeOAuthPools.resetBindingsConfirm'))) return
  await runAction(async () => {
    const poolId = detail.value!.pool.id
    await adminAPI.claudeOAuthPools.resetCredentialBindings(poolId, accountId)
    await selectPool(poolId)
  })
}

async function createAndActivateCapsule() {
  if (!detail.value) return
  await runAction(async () => {
    const poolId = detail.value!.pool.id
    await adminAPI.claudeOAuthPools.createCapsule(poolId, { ...capsule })
    await adminAPI.claudeOAuthPools.activateCapsule(poolId, capsule.version)
    await loadPools()
  })
}

async function setMode(mode: ClaudeOAuthPoolMode) {
  if (!detail.value) return
  await runAction(async () => {
    await adminAPI.claudeOAuthPools.setMode(detail.value!.pool.id, mode)
    await loadPools()
  })
}

async function runAction(action: () => Promise<void>) {
  saving.value = true
  try {
    await action()
    appStore.showSuccess(t('common.success'))
  } catch (error: any) {
    appStore.showError(error?.message || t('common.unknownError'))
  } finally {
    saving.value = false
  }
}

onMounted(loadPools)
</script>
