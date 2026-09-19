<script setup lang="ts">
import { formatCount } from '~/constants/store'
import type { AdminCoupon } from '~~/shared/types/store'

defineProps<{ coupons: AdminCoupon[] }>()

const th = 'px-3 py-2 text-left text-xs font-medium tracking-wider text-default-400'
const td = 'whitespace-nowrap px-3 py-2 text-sm'
</script>

<template>
  <div class="overflow-x-auto rounded-lg border border-default-200">
    <table class="w-full">
      <thead class="border-b border-default-200 bg-default-50">
        <tr>
          <th :class="th">券码</th>
          <th :class="[th, 'text-right']">面额</th>
          <th :class="th">有效期至</th>
          <th :class="th">分给</th>
          <th :class="th">已发放</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-default-200">
        <tr v-for="c in coupons" :key="c.id">
          <td :class="td">
            <KunCopy :text="c.code" size="sm" />
          </td>
          <td :class="[td, 'text-right']">{{ formatCount(c.face_value) }} 点</td>
          <td :class="[td, 'text-default-500']">{{ c.expires_on ?? '—' }}</td>
          <td :class="td">
            <span v-if="c.user_id !== null" class="text-foreground">
              {{ c.user_name || `用户 #${c.user_id}` }}
            </span>
            <span v-else class="text-default-400">未分配（留在平台）</span>
          </td>
          <td :class="td">
            <KunChip v-if="c.delivered_at" color="success" variant="flat" size="xs">已发放</KunChip>
            <span v-else class="text-default-300">—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
