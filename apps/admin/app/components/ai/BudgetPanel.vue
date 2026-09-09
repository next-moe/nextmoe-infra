<script setup lang="ts">
import { AI_ROUTE_MODERATE_TEXT } from '~/constants/ai'
import type { AiBudget } from '~~/shared/types/ai'

const api = useApi('ai')

const { data, refresh } = await useApiFetch<AiBudget[]>(
  '/admin/ai/budgets',
  {},
  'ai'
)
const budgets = computed<AiBudget[]>(() => data.value ?? [])

const route = ref(AI_ROUTE_MODERATE_TEXT)
const site = ref('')
const capStr = ref('')
const saving = ref(false)

const fmtInt = (n?: number | null) => (n ?? 0).toLocaleString()

const upsert = async (
  targetRoute: string,
  targetSite: string,
  cap: number | null
) => {
  saving.value = true
  try {
    const res = await api.put('/admin/ai/budgets', {
      route: targetRoute,
      site: targetSite,
      daily_cost_cap_micro: cap
    })
    if (res.code === 0) {
      useKunMessage('预算已保存', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '保存失败', 'error')
    }
  } finally {
    saving.value = false
  }
}

const submit = async () => {
  const r = route.value.trim()
  if (!r) {
    useKunMessage('路由不能为空', 'warn')
    return
  }
  const raw = capStr.value.trim()
  let cap: number | null = null
  if (raw !== '') {
    const n = Number(raw)
    if (!Number.isFinite(n) || n < 0) {
      useKunMessage('上限须为非负数(留空为清帽)', 'warn')
      return
    }
    cap = Math.floor(n)
  }
  await upsert(r, site.value.trim(), cap)
}

const clearRow = (b: AiBudget) => upsert(b.route, b.site, null)
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-foreground text-lg font-semibold">预算保险丝</h2>

    <div class="bg-content1 space-y-4 rounded-xl p-4 shadow-sm">
      <div class="flex flex-wrap items-end gap-2">
        <div class="flex flex-col gap-1">
          <span class="text-default-500 text-xs">路由</span>
          <KunInput
            v-model="route"
            class="w-48"
            placeholder="moderate-text"
            aria-label="路由"
          />
        </div>
        <div class="flex flex-col gap-1">
          <span class="text-default-500 text-xs">站点(留空=全局)</span>
          <KunInput
            v-model="site"
            class="w-44"
            placeholder="全局默认"
            aria-label="站点"
          />
        </div>
        <div class="flex flex-col gap-1">
          <span class="text-default-500 text-xs">每日成本上限 (µ · 留空=清帽)</span>
          <KunInput
            v-model="capStr"
            type="number"
            min="0"
            inputmode="numeric"
            placeholder="不设上限"
            aria-label="每日成本上限(微币,留空为清帽)"
            class="w-52"
          />
        </div>
        <KunButton
          color="primary"
          variant="flat"
          :loading="saving"
          @click="submit"
        >
          保存
        </KunButton>
      </div>

      <p class="text-default-400 text-xs">
        v0「先记不拦」:上限仅记录水位,不阻断调用;帽值留空(NULL)= 不拦。
      </p>

      <div class="overflow-x-auto">
        <table class="w-full min-w-[40rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-3 py-2 text-left font-medium">路由</th>
              <th class="px-3 py-2 text-left font-medium">站点</th>
              <th class="px-3 py-2 text-right font-medium">每日上限 (µ)</th>
              <th class="px-3 py-2 text-left font-medium">更新时间</th>
              <th class="px-3 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="b in budgets"
              :key="`${b.route}-${b.site}`"
              class="border-default-200 border-t"
            >
              <td class="px-3 py-2">
                <KunChip color="info" variant="flat" size="xs">{{ b.route }}</KunChip>
              </td>
              <td class="px-3 py-2">
                <span v-if="b.site">{{ b.site }}</span>
                <span v-else class="text-default-400">全局默认</span>
              </td>
              <td class="px-3 py-2 text-right tabular-nums">
                <span v-if="b.daily_cost_cap_micro != null">{{ fmtInt(b.daily_cost_cap_micro) }}</span>
                <span v-else class="text-default-400">未设(不拦)</span>
              </td>
              <td class="text-default-400 px-3 py-2">
                {{ new Date(b.updated_at).toLocaleString() }}
              </td>
              <td class="px-3 py-2 text-right">
                <KunButton
                  v-if="b.daily_cost_cap_micro != null"
                  color="danger"
                  variant="light"
                  size="sm"
                  :loading="saving"
                  @click="clearRow(b)"
                >
                  清帽
                </KunButton>
              </td>
            </tr>
            <tr v-if="!budgets.length">
              <td colspan="5" class="text-default-400 px-3 py-8 text-center">
                还没有预算行(v0 默认全部不拦)
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
