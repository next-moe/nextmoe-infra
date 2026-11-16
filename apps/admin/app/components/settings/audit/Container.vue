<script setup lang="ts">
import type {
  EnvAuditEntry,
  SettingKind,
  SettingsAuditEntry,
  SettingsOverview
} from '~~/shared/types/settings'
import { AUDIT_LIST_LIMIT } from '~/constants/settings'

const AUDIT_TABS = [
  { value: 'runtime', textValue: '运行期配置' },
  { value: 'env', textValue: '环境变量与密钥' }
]

const tab = ref('runtime')

const { data: sitesData } = await useApiFetch<Site[]>('/sites')
const siteNames = computed<Record<string, string>>(() =>
  Object.fromEntries(
    (sitesData.value ?? []).map((site) => [String(site.id), site.name])
  )
)

const { data: overviewData } =
  await useApiFetch<SettingsOverview>('/admin/settings')
const kinds = computed<Record<string, SettingKind>>(() =>
  Object.fromEntries(
    (overviewData.value?.domains ?? []).flatMap((domain) =>
      domain.keys.map((row) => [row.key, row.kind])
    )
  )
)

const {
  data: runtimeData,
  refresh: refreshRuntime,
  error: runtimeError
} = await useApiFetch<SettingsAuditEntry[]>('/admin/settings/audit', {
  query: { limit: AUDIT_LIST_LIMIT }
})

const {
  data: envData,
  refresh: refreshEnv,
  error: envError
} = await useApiFetch<EnvAuditEntry[]>('/admin/settings/env/audit', {
  query: { limit: 50 }
})
</script>

<template>
  <div class="space-y-6">
    <SettingsNav
      title="变更记录"
      description="配置中心每一次写入与撤销;密钥只记录指纹,不记录值。"
    />

    <KunTab v-model="tab" :items="AUDIT_TABS" variant="pills" />

    <SettingsAuditList
      v-if="tab === 'runtime'"
      :entries="runtimeData ?? []"
      :has-error="Boolean(runtimeError)"
      :kinds="kinds"
      :site-names="siteNames"
      @retry="refreshRuntime"
    />

    <SettingsEnvAuditList
      v-else
      :entries="envData ?? []"
      :has-error="Boolean(envError)"
      @retry="refreshEnv"
    />
  </div>
</template>
