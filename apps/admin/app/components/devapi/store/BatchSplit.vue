<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { formatCount, formatShare } from '~/constants/store'
import type { CouponSplitRow, CouponValueCount } from '~~/shared/types/store'

const props = defineProps<{
  rows: CouponSplitRow[]
  values: CouponValueCount[]
  readonly: boolean
}>()

const counts = defineModel<Record<string, Record<number, number>>>({ required: true })

const faces = computed(() => props.values.map((v) => v.face_value))
const eligible = computed(() => props.rows.filter((r) => r.settlement_eligible))
const excluded = computed(() => props.rows.filter((r) => !r.settlement_eligible))

const countOf = (clientId: string, face: number) => counts.value[clientId]?.[face] ?? 0

const setCount = (clientId: string, face: number, n: number | null) => {
  counts.value = {
    ...counts.value,
    [clientId]: { ...counts.value[clientId], [face]: Math.max(n ?? 0, 0) },
  }
}

const pointsOf = (clientId: string) =>
  faces.value.reduce((sum, face) => sum + face * countOf(clientId, face), 0)

const usedOf = (face: number) =>
  eligible.value.reduce((n, r) => n + countOf(r.client_id, face), 0)

const diffClass = (row: CouponSplitRow) => {
  const diff = pointsOf(row.client_id) - row.entitled_points
  return cn(
    'text-right',
    diff > 0 ? 'text-warning-600' : diff < 0 ? 'text-default-400' : 'text-success-600'
  )
}

const signed = (n: number) => `${n > 0 ? '+' : ''}${formatCount(n)}`

const th = 'px-3 py-2 text-left text-xs font-medium tracking-wider text-default-400'
const td = 'whitespace-nowrap px-3 py-2 text-sm'
</script>

<template>
  <div class="space-y-3">
    <div class="overflow-x-auto rounded-lg border border-default-200">
      <table class="w-full">
        <thead class="border-b border-default-200 bg-default-50">
          <tr>
            <th :class="th">站点</th>
            <th :class="[th, 'text-right']">去重点击</th>
            <th :class="[th, 'text-right']">占比</th>
            <th :class="[th, 'text-right']">应得（点）</th>
            <th v-for="face in faces" :key="face" :class="[th, 'text-center']">
              {{ formatCount(face) }} 点
            </th>
            <th :class="[th, 'text-right']">分到（点）</th>
            <th :class="[th, 'text-right']">差额</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default-200">
          <tr v-if="!eligible.length">
            <td :colspan="faces.length + 6" class="py-6 text-center text-sm text-default-400">
              结算区间里没有参与分成且有点击的站点，先在上方表格里打开「参与分成」
            </td>
          </tr>
          <tr v-for="row in eligible" :key="row.client_id">
            <td :class="[td, 'font-medium text-foreground']">{{ row.name }}</td>
            <td :class="[td, 'text-right']">{{ formatCount(row.uniques) }}</td>
            <td :class="[td, 'text-right text-default-500']">{{ formatShare(row.share_ppm) }}</td>
            <td :class="[td, 'text-right']">{{ formatCount(row.entitled_points) }}</td>
            <td v-for="face in faces" :key="face" :class="[td, 'text-center']">
              <span v-if="readonly">{{ countOf(row.client_id, face) }}</span>
              <KunNumberInput
                v-else
                :model-value="countOf(row.client_id, face)"
                :min="0"
                size="sm"
                class="mx-auto w-24"
                :aria-label="`${row.name} 的 ${face} 点券张数`"
                @update:model-value="(n: number | null) => setCount(row.client_id, face, n)"
              />
            </td>
            <td :class="[td, 'text-right font-semibold text-foreground']">
              {{ formatCount(pointsOf(row.client_id)) }}
            </td>
            <td :class="[td, diffClass(row)]">
              {{ signed(pointsOf(row.client_id) - row.entitled_points) }}
            </td>
          </tr>
        </tbody>
        <tfoot class="border-t border-default-200 bg-default-50">
          <tr>
            <td :class="[td, 'text-default-500']" colspan="4">已分 / 共有</td>
            <td
              v-for="v in values"
              :key="v.face_value"
              :class="[td, 'text-center', usedOf(v.face_value) > v.count ? 'font-semibold text-danger' : 'text-default-500']"
            >
              {{ usedOf(v.face_value) }} / {{ v.count }}
            </td>
            <td colspan="2" />
          </tr>
        </tfoot>
      </table>
    </div>

    <div v-if="excluded.length" class="rounded-lg bg-default-50 p-3 text-sm text-default-500">
      <p class="mb-1 font-medium text-default-600">有点击但没参与分成（不分券）</p>
      <p v-for="row in excluded" :key="row.client_id">
        {{ row.name }} · 去重点击 {{ formatCount(row.uniques) }}
      </p>
    </div>
  </div>
</template>
