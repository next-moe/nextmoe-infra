<script setup lang="ts">
import { formatCount, formatShare } from '~/constants/store'
import type { StoreUsageApp } from '~~/shared/types/store'

defineProps<{ apps: StoreUsageApp[] }>()
const emit = defineEmits<{ changed: [] }>()

const api = useApi()
const saving = ref<string | null>(null)

const setEligible = async (app: StoreUsageApp, eligible: boolean) => {
  saving.value = app.client_id
  try {
    const res = await api.patch(`/admin/devapi/apps/${app.client_id}`, {
      store_settlement_eligible: eligible,
    })
    if (res.code === 0) {
      useKunMessage(eligible ? `${app.name} 已加入分成名单` : `${app.name} 已移出分成名单`, 'success')
      emit('changed')
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    saving.value = null
  }
}

const th = 'px-4 py-3 text-left text-xs font-medium tracking-wider text-default-400'
const td = 'whitespace-nowrap px-4 py-3 text-sm'
</script>

<template>
  <div class="space-y-2">
    <div>
      <h2 class="text-lg font-semibold text-foreground">各站点击</h2>
      <p class="text-sm text-default-500">
        占比按全部站点的去重点击算。分券按用户：同一用户名下打开「参与分成」的应用合并计算；新建的应用默认参与。
      </p>
    </div>
    <div class="overflow-x-auto rounded-xl bg-content1 shadow-sm">
      <div v-if="apps.length === 0" class="py-12 text-center">
        <KunIcon name="lucide:link" class="mx-auto mb-4 size-12 text-default-200" />
        <p class="text-default-400">还没有站点铸过分销短链</p>
      </div>
      <table v-else class="w-full">
        <thead class="border-b border-default-200 bg-default-50">
          <tr>
            <th :class="th">站点</th>
            <th :class="th">参与分成</th>
            <th :class="[th, 'text-right']">短链</th>
            <th :class="[th, 'text-right']">去重点击</th>
            <th :class="[th, 'text-right']">占比</th>
            <th :class="[th, 'text-right']">爬虫</th>
            <th :class="[th, 'text-right']">总点击</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default-200">
          <tr v-for="app in apps" :key="app.client_id" class="hover:bg-default-100">
            <td :class="td">
              <p class="font-medium text-foreground">{{ app.name }}</p>
              <p class="text-xs text-default-400">
                <span v-if="app.owner_user_id !== null">
                  {{ app.owner_name || `用户 #${app.owner_user_id}` }}
                </span>
                <span v-else class="text-warning-600">没有归属用户，不分券</span>
                · <span class="font-mono">{{ app.client_id }}</span>
              </p>
            </td>
            <td :class="td">
              <KunSwitch
                :model-value="app.settlement_eligible"
                :disabled="saving === app.client_id"
                size="sm"
                @update:model-value="(v: boolean) => setEligible(app, v)"
              />
            </td>
            <td :class="[td, 'text-right text-default-500']">{{ formatCount(app.links) }}</td>
            <td :class="[td, 'text-right font-semibold text-foreground']">
              {{ formatCount(app.uniques) }}
            </td>
            <td :class="[td, 'w-40']">
              <div class="flex items-center justify-end gap-2">
                <KunProgress
                  :value="app.share_ppm"
                  :max="1_000_000"
                  size="sm"
                  color="primary"
                  class-name="w-16"
                  aria-label="点击占比"
                />
                <span class="w-14 text-right text-default-500">{{ formatShare(app.share_ppm) }}</span>
              </div>
            </td>
            <td :class="[td, 'text-right text-default-400']">{{ formatCount(app.bots) }}</td>
            <td :class="[td, 'text-right text-default-500']">{{ formatCount(app.total) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
