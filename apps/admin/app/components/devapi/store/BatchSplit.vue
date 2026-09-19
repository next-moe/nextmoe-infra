<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { formatCount, formatShare } from '~/constants/store'
import type {
  CouponExcludedApp,
  CouponSplitRow,
  CouponValueCount,
} from '~~/shared/types/store'

const props = defineProps<{
  rows: CouponSplitRow[]
  excluded: CouponExcludedApp[]
  values: CouponValueCount[]
  readonly: boolean
}>()

const counts = defineModel<Record<number, Record<number, number>>>({ required: true })

const faces = computed(() => props.values.map((v) => v.face_value))

const countOf = (userId: number, face: number) => counts.value[userId]?.[face] ?? 0

const setCount = (userId: number, face: number, n: number | null) => {
  counts.value = {
    ...counts.value,
    [userId]: { ...counts.value[userId], [face]: Math.max(n ?? 0, 0) },
  }
}

const pointsOf = (userId: number) =>
  faces.value.reduce((sum, face) => sum + face * countOf(userId, face), 0)

const usedOf = (face: number) => props.rows.reduce((n, r) => n + countOf(r.user_id, face), 0)

const appsLine = (row: CouponSplitRow) =>
  row.apps.map((a) => `${a.name} ${formatCount(a.uniques)}`).join(' · ')

const excludedReason = (app: CouponExcludedApp) =>
  app.owner_user_id === null ? '没有归属用户' : '未参与分成'

const diffClass = (row: CouponSplitRow) => {
  const diff = pointsOf(row.user_id) - row.entitled_points
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
            <th :class="th">用户</th>
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
          <tr v-if="!rows.length">
            <td :colspan="faces.length + 6" class="py-6 text-center text-sm text-default-400">
              结算区间里没有参与分成且有点击的用户，先在上方表格里打开「参与分成」
            </td>
          </tr>
          <tr v-for="row in rows" :key="row.user_id">
            <td :class="td">
              <p class="font-medium text-foreground">{{ row.name }}</p>
              <p class="text-xs text-default-400">{{ appsLine(row) }}</p>
            </td>
            <td :class="[td, 'text-right']">{{ formatCount(row.uniques) }}</td>
            <td :class="[td, 'text-right text-default-500']">{{ formatShare(row.share_ppm) }}</td>
            <td :class="[td, 'text-right']">{{ formatCount(row.entitled_points) }}</td>
            <td v-for="face in faces" :key="face" :class="[td, 'text-center']">
              <span v-if="readonly">{{ countOf(row.user_id, face) }}</span>
              <KunNumberInput
                v-else
                :model-value="countOf(row.user_id, face)"
                :min="0"
                size="sm"
                class="mx-auto w-24"
                :aria-label="`${row.name} 的 ${face} 点券张数`"
                @update:model-value="(n: number | null) => setCount(row.user_id, face, n)"
              />
            </td>
            <td :class="[td, 'text-right font-semibold text-foreground']">
              {{ formatCount(pointsOf(row.user_id)) }}
            </td>
            <td :class="[td, diffClass(row)]">
              {{ signed(pointsOf(row.user_id) - row.entitled_points) }}
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
      <p class="mb-1 font-medium text-default-600">有点击但不计入分成（不分券）</p>
      <p v-for="app in excluded" :key="app.client_id">
        {{ app.name }} · 去重点击 {{ formatCount(app.uniques) }} · {{ excludedReason(app) }}
      </p>
    </div>
  </div>
</template>
