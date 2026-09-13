<script setup lang="ts">
import { REVIEW_STATUS, CALLBACK_STATUS } from '~/constants/trust'
import type {
  TrustReviewItemPage,
  TrustDispositionPage
} from '~~/shared/types/trust'

const route = useRoute()

const { data: pendingItems } = await useApiFetch<TrustReviewItemPage>(
  '/admin/trust/review-items',
  { query: { status: REVIEW_STATUS.pending, limit: 1 } },
  'trust'
)
const { data: deadLetters } = await useApiFetch<TrustDispositionPage>(
  '/admin/trust/dispositions',
  { query: { callback_status: CALLBACK_STATUS.deadLetter, limit: 1 } },
  'trust'
)

const tabs = computed(() => [
  {
    to: '/trust',
    label: '审核队列',
    icon: 'lucide:inbox',
    count: pendingItems.value?.total ?? 0
  },
  {
    to: '/trust/registries',
    label: '注册表',
    icon: 'lucide:list-checks',
    count: 0
  },
  {
    to: '/trust/terms',
    label: 'Tier0 词表',
    icon: 'lucide:spell-check',
    count: 0
  },
  {
    to: '/trust/site-policies',
    label: '站点策略',
    icon: 'lucide:sliders-horizontal',
    count: 0
  },
  {
    to: '/trust/dead-letters',
    label: '回调死信',
    icon: 'lucide:mail-warning',
    count: deadLetters.value?.total ?? 0
  }
])
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <KunButton
      v-for="tab in tabs"
      :key="tab.to"
      :color="route.path === tab.to ? 'primary' : 'default'"
      :variant="route.path === tab.to ? 'solid' : 'flat'"
      size="sm"
      @click="navigateTo(tab.to)"
    >
      <KunIcon :name="tab.icon" class="mr-1 size-4" />
      {{ tab.label }}
      <KunChip
        v-if="tab.count > 0"
        :color="route.path === tab.to ? 'default' : 'warning'"
        variant="flat"
        size="xs"
        class="ml-2"
      >
        {{ tab.count }}
      </KunChip>
    </KunButton>
  </div>
</template>
