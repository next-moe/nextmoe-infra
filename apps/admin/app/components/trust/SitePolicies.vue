<script setup lang="ts">
import type {
  TrustSitePolicy,
  TrustSitePoliciesResponse,
  TrustUpsertSitePolicyRequest
} from '~~/shared/types/trust'
import {
  SCAN_MODE,
  SCAN_MODE_LABELS,
  SCAN_MODE_COLORS
} from '~/constants/trust'

const api = useApi('trust')

const { data, status, refresh, error } =
  await useApiFetch<TrustSitePoliciesResponse>(
    '/admin/trust/site-policies',
    {},
    'trust'
  )

const policies = computed<TrustSitePolicy[]>(() => data.value?.policies ?? [])
const defaults = computed(() => data.value?.defaults)
const isLoading = computed(() => status.value === 'pending')

const formatRate = (v: number) => `${(v * 100).toFixed(2)}%`

const inheritedLabel = (kind: 'mode' | 'rate' | 'aggregate' | 'hide') => {
  const d = defaults.value
  if (!d) return '继承'
  if (kind === 'mode') return `继承(${SCAN_MODE_LABELS[d.scan_mode]})`
  if (kind === 'rate') return `继承(${formatRate(d.sample_rate)})`
  if (kind === 'aggregate') return `继承(${d.aggregate_threshold})`
  return `继承(${d.auto_hide_enabled ? '开' : '关'})`
}

const editOpen = ref(false)
const saving = ref(false)
const form = reactive({
  site: '',
  sampleRate: '',
  flagThreshold: '',
  aggregateThreshold: '',
  note: ''
})

const isNew = ref(false)

const scanModeOptions = computed(() => [
  { value: -1, label: inheritedLabel('mode') },
  { value: SCAN_MODE.shadow, label: SCAN_MODE_LABELS[SCAN_MODE.shadow]! },
  { value: SCAN_MODE.live, label: SCAN_MODE_LABELS[SCAN_MODE.live]! }
])
const autoHideOptions = computed(() => [
  { value: -1, label: inheritedLabel('hide') },
  { value: 1, label: '开(可自动隐藏)' },
  { value: 0, label: '关(只开单,不隐藏)' }
])

const scanModeSelect = ref(-1)
const autoHideSelect = ref(-1)

const openEdit = (p?: TrustSitePolicy) => {
  isNew.value = !p
  form.site = p?.site ?? ''
  scanModeSelect.value = p?.scan_mode ?? -1
  form.sampleRate = p?.sample_rate != null ? String(p.sample_rate) : ''
  form.flagThreshold = p?.flag_threshold != null ? String(p.flag_threshold) : ''
  form.aggregateThreshold =
    p?.aggregate_threshold != null ? String(p.aggregate_threshold) : ''
  autoHideSelect.value =
    p?.auto_hide_enabled == null ? -1 : p.auto_hide_enabled ? 1 : 0
  form.note = p?.note ?? ''
  editOpen.value = true
}

const numberOrNull = (raw: string): number | null | 'invalid' => {
  const s = raw.trim()
  if (!s) return null
  const n = Number(s)
  return Number.isFinite(n) ? n : 'invalid'
}

const save = async () => {
  if (!form.site.trim()) {
    useKunMessage('站点必填', 'warn')
    return
  }
  const rate = numberOrNull(form.sampleRate)
  const flag = numberOrNull(form.flagThreshold)
  const aggregate = numberOrNull(form.aggregateThreshold)
  if (rate === 'invalid' || flag === 'invalid' || aggregate === 'invalid') {
    useKunMessage('数值字段填写有误,请填数字或留空(继承)', 'warn')
    return
  }

  saving.value = true
  try {
    const body: TrustUpsertSitePolicyRequest = {
      scan_mode: scanModeSelect.value === -1 ? undefined : scanModeSelect.value,
      sample_rate: rate ?? undefined,
      flag_threshold: flag ?? undefined,
      aggregate_threshold: aggregate ?? undefined,
      auto_hide_enabled:
        autoHideSelect.value === -1 ? undefined : autoHideSelect.value === 1,
      note: form.note.trim() || undefined
    }
    const res = await api.put<TrustSitePolicy>(
      `/admin/trust/site-policies/${encodeURIComponent(form.site.trim())}`,
      body
    )
    if (res.code === 0) {
      useKunMessage('已保存', 'success')
      editOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '保存失败', 'error')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">T&S 审核队列</h1>
    <TrustSubNav />
    <KunCard content-class="space-y-3 p-4">
      <div class="flex flex-wrap items-center gap-3">
        <h2 class="text-foreground text-lg font-bold">站点策略</h2>
        <span class="text-default-400 text-sm">
          未列出的站点全部继承平台默认
        </span>
        <KunButton
          color="primary"
          size="sm"
          class="ml-auto"
          @click="openEdit()"
        >
          <KunIcon name="lucide:plus" class="mr-1 size-4" />
          新增站点策略
        </KunButton>
      </div>

      <div
        v-if="defaults"
        class="border-default-200 text-default-500 rounded-lg border p-3 text-sm"
      >
        <span class="text-foreground font-medium">平台默认</span>
        ·
        <span>执法姿态 {{ SCAN_MODE_LABELS[defaults.scan_mode] }}</span>
        ·
        <span>抽检 {{ formatRate(defaults.sample_rate) }}</span>
        ·
        <span>举报聚合阈值 {{ defaults.aggregate_threshold }}</span>
        ·
        <span>自动隐藏 {{ defaults.auto_hide_enabled ? '开' : '关' }}</span>
      </div>

      <CommonFetchError v-if="error" @retry="refresh" />

      <div class="overflow-x-auto">
        <table class="w-full min-w-[52rem] text-sm">
          <thead class="text-default-500">
            <tr>
              <th class="px-2 py-2 text-left font-medium">站点</th>
              <th class="px-2 py-2 text-left font-medium">执法姿态</th>
              <th class="px-2 py-2 text-left font-medium">抽检比例</th>
              <th class="px-2 py-2 text-left font-medium">判定阈值</th>
              <th class="px-2 py-2 text-left font-medium">举报聚合</th>
              <th class="px-2 py-2 text-left font-medium">自动隐藏</th>
              <th class="px-2 py-2 text-left font-medium">备注</th>
              <th class="px-2 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="p in policies"
              :key="p.site"
              class="border-default-200 border-t align-top"
            >
              <td class="px-2 py-2 font-mono">{{ p.site }}</td>
              <td class="px-2 py-2">
                <KunChip
                  v-if="p.scan_mode != null"
                  :color="SCAN_MODE_COLORS[p.scan_mode]"
                  variant="flat"
                  size="xs"
                >
                  {{ SCAN_MODE_LABELS[p.scan_mode] }}
                </KunChip>
                <span v-else class="text-default-400">
                  {{ inheritedLabel('mode') }}
                </span>
              </td>
              <td class="px-2 py-2">
                <span v-if="p.sample_rate != null">
                  {{ formatRate(p.sample_rate) }}
                </span>
                <span v-else class="text-default-400">
                  {{ inheritedLabel('rate') }}
                </span>
              </td>
              <td class="px-2 py-2">
                <span v-if="p.flag_threshold != null">
                  {{ p.flag_threshold }}
                </span>
                <span v-else class="text-default-400">网关判定</span>
              </td>
              <td class="px-2 py-2">
                <span v-if="p.aggregate_threshold != null">
                  {{ p.aggregate_threshold }}
                </span>
                <span v-else class="text-default-400">
                  {{ inheritedLabel('aggregate') }}
                </span>
              </td>
              <td class="px-2 py-2">
                <KunChip
                  v-if="p.auto_hide_enabled != null"
                  :color="p.auto_hide_enabled ? 'warning' : 'success'"
                  variant="flat"
                  size="xs"
                >
                  {{ p.auto_hide_enabled ? '开' : '关' }}
                </KunChip>
                <span v-else class="text-default-400">
                  {{ inheritedLabel('hide') }}
                </span>
              </td>
              <td class="text-default-400 max-w-[14rem] truncate px-2 py-2">
                {{ p.note || '—' }}
              </td>
              <td class="px-2 py-2 text-right">
                <KunButton
                  color="primary"
                  variant="flat"
                  size="sm"
                  @click="openEdit(p)"
                >
                  编辑
                </KunButton>
              </td>
            </tr>
            <tr v-if="!policies.length && !error">
              <td colspan="8" class="text-default-400 px-2 py-8 text-center">
                {{
                  isLoading
                    ? '加载中…'
                    : '暂无站点策略 — 所有站点均按平台默认运行'
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </KunCard>

    <KunModal
      v-model="editOpen"
      size="lg"
      :title="isNew ? '新增站点策略' : '编辑站点策略'"
    >
      <div class="space-y-3">
        <p class="text-default-500 text-sm">
          留空 =
          继承平台默认。这与「填成和默认一样的值」不同:以后平台默认变了,继承的站点跟着变,写死的站点不变。
        </p>
        <KunInput
          v-model="form.site"
          :disabled="!isNew"
          placeholder="站点(kungal / moyu / letmoe …)"
        />
        <KunSelect v-model="scanModeSelect" :options="scanModeOptions" />
        <KunInput
          v-model="form.sampleRate"
          placeholder="抽检比例,0-0.05,留空继承(例:0.005 = 五百分之一)"
        />
        <KunInput
          v-model="form.flagThreshold"
          placeholder="判定阈值 0-1,留空 = 采用网关自己的判定"
        />
        <KunInput
          v-model="form.aggregateThreshold"
          placeholder="举报聚合阈值,留空继承"
        />
        <KunSelect v-model="autoHideSelect" :options="autoHideOptions" />
        <KunInput v-model="form.note" placeholder="备注:这个站为什么这样设" />
      </div>
      <template #footer>
        <KunButton color="default" variant="flat" @click="editOpen = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          保存
        </KunButton>
      </template>
    </KunModal>
  </div>
</template>
