<script setup lang="ts">
import type { EnvOverview, EnvVarView } from '~~/shared/types/settings'
import {
  ENV_FILTERS,
  ENV_GROUP_LABELS,
  ENV_GROUP_ORDER,
  ENV_PRECEDENCE_NOTE,
  envGroupOf,
  matchesEnvFilter,
  type EnvFilter
} from '~/constants/settings-env'

const CONFLICT_MESSAGE = '该配置已被其他人修改,请刷新后重试'

const api = useApi()

const { data, status, refresh, error } = await useApiFetch<EnvOverview>(
  '/admin/settings/env'
)

const overview = computed(() => data.value)
const vars = computed(() => overview.value?.vars ?? [])
const isLoading = computed(() => status.value === 'pending' && !data.value)

const group = ref('all')
const filter = ref<EnvFilter>('all')
const search = ref('')

const groupTabs = computed(() => {
  const counts: Record<string, number> = {}
  for (const row of vars.value) {
    const g = envGroupOf(row.key)
    counts[g] = (counts[g] ?? 0) + 1
  }
  return [
    { value: 'all', textValue: `全部 ${vars.value.length}` },
    ...ENV_GROUP_ORDER.filter((g) => counts[g]).map((g) => ({
      value: g,
      textValue: `${ENV_GROUP_LABELS[g]} ${counts[g]}`
    }))
  ]
})

const rows = computed(() => {
  const needle = search.value.trim().toUpperCase()
  return vars.value.filter(
    (row) =>
      (group.value === 'all' || envGroupOf(row.key) === group.value) &&
      matchesEnvFilter(row, filter.value) &&
      (needle === '' || row.key.includes(needle))
  )
})

const issueCount = computed(
  () => vars.value.filter((row) => row.issues.length > 0).length
)

const editOpen = ref(false)
const resetOpen = ref(false)
const pendingRow = ref<EnvVarView | null>(null)
const pendingScope = ref('')
const submitting = ref(false)

const varPath = (key: string) =>
  `/admin/settings/env/${encodeURIComponent(key)}`

const askEdit = (row: EnvVarView) => {
  pendingRow.value = row
  editOpen.value = true
}

const askReset = (row: EnvVarView, scope: string) => {
  pendingRow.value = row
  pendingScope.value = scope
  resetOpen.value = true
}

const handleWriteError = async (message: string) => {
  useKunMessage(message || '操作失败', 'error')
  if (message === CONFLICT_MESSAGE) await refresh()
}

const save = async (scope: string, value: string, note: string) => {
  const row = pendingRow.value
  if (!row) return
  submitting.value = true
  try {
    const current = row.stored.find((s) => s.scope === scope)
    const response = await api.put(varPath(row.key), {
      scope,
      value,
      note,
      ...(current ? { version: current.version } : {})
    })
    if (response.code === 0) {
      useKunMessage('已写入,重启对应服务后生效', 'success')
      editOpen.value = false
      await refresh()
    } else {
      await handleWriteError(response.message)
    }
  } finally {
    submitting.value = false
  }
}

const confirmReset = async (note: string) => {
  const row = pendingRow.value
  if (!row) return
  submitting.value = true
  try {
    const response = await api.delete(varPath(row.key), {
      scope: pendingScope.value,
      note
    })
    if (response.code === 0) {
      useKunMessage('已撤销,重启对应服务后生效', 'success')
      resetOpen.value = false
      await refresh()
    } else {
      await handleWriteError(response.message)
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <SettingsNav title="环境变量与密钥" :description="ENV_PRECEDENCE_NOTE" />

    <CommonFetchError v-if="error" @retry="refresh" />

    <div v-else-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <template v-else-if="overview">
      <KunCard v-if="!overview.kek_configured" content-class="p-4">
        <p class="text-warning-600">
          oauth 启动时没有 KUN_CONFIG_KEK:只能查看,不能写入配置中心。
        </p>
      </KunCard>

      <SettingsEnvServiceList :services="overview.services" />

      <div class="space-y-3">
        <KunTab v-model="group" :items="groupTabs" variant="pills" />
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
          <KunSelect
            v-model="filter"
            label="筛选"
            :options="ENV_FILTERS"
            class-name="w-full sm:w-56"
          />
          <KunInput
            v-model="search"
            label="搜索变量名"
            placeholder="例如 S3_BUCKET"
            class-name="w-full sm:w-72"
          />
          <p
            v-if="issueCount"
            class="text-danger-600 text-sm whitespace-nowrap sm:mb-2 sm:ml-auto"
          >
            {{ issueCount }} 个变量需要处理
          </p>
        </div>
      </div>

      <SettingsEnvTable
        :rows="rows"
        :writable="overview.writable"
        @edit="askEdit"
        @reset="askReset"
      />

      <SettingsEnvEditModal
        v-model:open="editOpen"
        :row="pendingRow"
        :scopes="overview.scopes"
        :submitting="submitting"
        @save="save"
      />

      <SettingsEnvResetModal
        v-model:open="resetOpen"
        :row="pendingRow"
        :scope="pendingScope"
        :submitting="submitting"
        @confirm="confirmReset"
      />
    </template>
  </div>
</template>
