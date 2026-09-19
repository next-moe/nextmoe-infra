<script setup lang="ts">
import { dlsiteProductUrl, formatCount } from '~/constants/store'
import type { StoreUsageLink } from '~~/shared/types/store'

defineProps<{ links: StoreUsageLink[] }>()

const th = 'px-4 py-3 text-left text-xs font-medium tracking-wider text-default-400'
const td = 'whitespace-nowrap px-4 py-2.5 text-sm'
</script>

<template>
  <div class="space-y-2">
    <div>
      <h2 class="text-lg font-semibold text-foreground">点击最多的短链</h2>
      <p class="text-sm text-default-500">按去重点击排序，最多显示 100 条</p>
    </div>
    <div class="overflow-x-auto rounded-xl bg-content1 shadow-sm">
      <p v-if="links.length === 0" class="py-8 text-center text-default-400">这个区间没有点击</p>
      <table v-else class="w-full">
        <thead class="border-b border-default-200 bg-default-50">
          <tr>
            <th :class="th">商品 / 活动</th>
            <th :class="th">站点</th>
            <th :class="[th, 'text-right']">去重点击</th>
            <th :class="[th, 'text-right']">爬虫</th>
            <th :class="[th, 'text-right']">总点击</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default-200">
          <tr
            v-for="link in links"
            :key="`${link.client_id}:${link.kind}:${link.product_id ?? link.campaign_id}`"
            class="hover:bg-default-100"
          >
            <td :class="td">
              <KunLink
                v-if="link.product_id"
                :href="dlsiteProductUrl(link.product_id)"
                target="_blank"
                underline="hover"
                color="primary"
                class-name="font-mono"
              >
                {{ link.product_id }}
              </KunLink>
              <span v-else class="text-default-500">优惠券活动 #{{ link.campaign_id }}</span>
            </td>
            <td :class="[td, 'text-default-500']">{{ link.app_name }}</td>
            <td :class="[td, 'text-right font-semibold text-foreground']">
              {{ formatCount(link.uniques) }}
            </td>
            <td :class="[td, 'text-right text-default-400']">{{ formatCount(link.bots) }}</td>
            <td :class="[td, 'text-right text-default-500']">{{ formatCount(link.total) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
