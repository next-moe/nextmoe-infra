<script setup lang="ts">
import {
  TRUST_FILTER_ALL,
  CALLBACK_STATUS,
  CALLBACK_STATUS_LABELS,
  CALLBACK_STATUS_COLORS,
  ACTION_LABELS
} from '~/constants/trust'
import type { TrustDispositionPage } from '~~/shared/types/trust'

const api = useApi('trust')

const page = ref(1)
const limit = 50
const callbackStatus = ref<number>(CALLBACK_STATUS.deadLetter)
watch(callbackStatus, () => {
  page.value = 1
})

const { data, status: fetchStatus, refresh, error } =
  await useApiFetch<TrustDispositionPage>(
    '/admin/trust/dispositions',
    {
      query: computed(() => ({
        page: page.value,
        limit,
        callback_status: callbackStatus.value
      }))
    },
    'trust'
  )
const items = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => fetchStatus.value === 'pending')

const statusOptions = computed(() => [
  { value: TRUST_FILTER_ALL, label: '全部' },
  ...Object.entries(CALLBACK_STATUS_LABELS).map(([id, label]) => ({
    value: Number(id),
    label
  }))
])

const redelivering = ref(0)
const redeliver = async (id: number) => {
  redelivering.value = id
  try {
    const res = await api.post(`/admin/trust/dispositions/${id}/redeliver`)
    if (res.code === 0) {
      useKunMessage('已重投(重置为待投递)', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '重投失败', 'error')
    }
  } finally {
    redelivering.value = 0
  }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">T&S 审核队列</h1>
    <TrustSubNav />

    <div class="flex flex-wrap items-center gap-2">
      <KunSelect
        v-model="callbackStatus"
        :options="statusOptions"
        aria-label="回调状态过滤"
        class="w-40"
      />
    </div>
    <p class="text-default-500 text-sm">共 {{ total }} 条</p>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[56rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">处置</th>
            <th class="px-3 py-2 text-left font-medium">审核项</th>
            <th class="px-3 py-2 text-left font-medium">动作</th>
            <th class="px-3 py-2 text-left font-medium">理由</th>
            <th class="px-3 py-2 text-right font-medium">尝试</th>
            <th class="px-3 py-2 text-left font-medium">回调状态</th>
            <th class="px-3 py-2 text-left font-medium">时间</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="d in items"
            :key="d.id"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">#{{ d.id }}</td>
            <td class="px-3 py-2">#{{ d.review_item_id }}</td>
            <td class="px-3 py-2">{{ ACTION_LABELS[d.action] ?? d.action }}</td>
            <td class="px-3 py-2 font-mono">{{ d.reason_code }}</td>
            <td class="px-3 py-2 text-right">{{ d.callback_attempts }}</td>
            <td class="px-3 py-2">
              <KunChip
                v-if="d.callback_status != null"
                :color="CALLBACK_STATUS_COLORS[d.callback_status] ?? 'default'"
                variant="flat"
                size="xs"
              >
                {{ CALLBACK_STATUS_LABELS[d.callback_status] ?? d.callback_status }}
              </KunChip>
              <span v-else class="text-default-300">无回调</span>
            </td>
            <td class="text-default-400 px-3 py-2">
              {{ new Date(d.created_at).toLocaleString() }}
            </td>
            <td class="px-3 py-2 text-right">
              <KunButton
                v-if="d.callback_status === CALLBACK_STATUS.deadLetter"
                color="primary"
                variant="flat"
                size="sm"
                :loading="redelivering === d.id"
                @click="redeliver(d.id)"
              >
                重投
              </KunButton>
            </td>
          </tr>
          <tr v-if="!items.length && !error">
            <td colspan="8" class="text-default-400 px-3 py-10 text-center">
              {{ isLoading ? '加载中…' : '没有匹配的处置' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <KunPagination
      v-model:current-page="page"
      :total-page="totalPages"
      :is-loading="isLoading"
    />
  </div>
</template>
