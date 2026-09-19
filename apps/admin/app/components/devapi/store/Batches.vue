<script setup lang="ts">
import { COUPON_BATCH_STATUS, formatCount } from '~/constants/store'
import type { CouponBatchSummary } from '~~/shared/types/store'

defineProps<{ range: [string, string] }>()

const { data, status, refresh } = await useApiFetch<CouponBatchSummary[]>(
  '/admin/devapi/store/coupon-batches'
)
const batches = computed(() => data.value ?? [])

const createOpen = ref(false)
const detailOpen = ref(false)
const detailId = ref<number | null>(null)

const openDetail = (id: number) => {
  detailId.value = id
  detailOpen.value = true
}

const handleCreated = (id: number) => {
  createOpen.value = false
  refresh()
  openDetail(id)
}

const valueLine = (b: CouponBatchSummary) =>
  b.by_value.map((v) => `${formatCount(v.face_value)} 点 × ${v.count}`).join('，')
</script>

<template>
  <div class="space-y-2">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-foreground">优惠券批次</h2>
        <p class="text-sm text-default-500">
          录入 DLsite 给的券码，按结算区间内的去重点击占比分给参与分成的站点；发布后各站站长在开发者平台看到自己的券码。
        </p>
      </div>
      <KunButton color="primary" @click="createOpen = true">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        录入一批券
      </KunButton>
    </div>

    <div v-if="status === 'pending' && !batches.length" class="flex justify-center py-8">
      <KunIcon name="lucide:loader-circle" class="size-6 animate-spin text-primary" />
    </div>
    <div v-else-if="!batches.length" class="rounded-xl bg-content1 py-10 text-center shadow-sm">
      <KunIcon name="lucide:ticket" class="mx-auto mb-3 size-10 text-default-200" />
      <p class="text-default-400">还没有录入过优惠券</p>
    </div>

    <div v-else class="space-y-3">
      <KunCard
        v-for="b in batches"
        :key="b.id"
        content-class="justify-start gap-0"
        class-name="p-4"
        clickable
        is-hoverable
        @click="openDetail(b.id)"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-semibold text-foreground">{{ b.name }}</span>
              <KunChip :color="COUPON_BATCH_STATUS[b.status].color" variant="flat" size="xs">
                {{ COUPON_BATCH_STATUS[b.status].label }}
              </KunChip>
            </div>
            <p class="mt-1 text-sm text-default-500">
              结算区间 {{ b.period_from }} ~ {{ b.period_to }} · {{ valueLine(b) }}
            </p>
          </div>
          <div class="text-right text-sm">
            <p class="font-semibold text-foreground">{{ formatCount(b.points) }} 点</p>
            <p class="text-default-400">
              <template v-if="b.status === 'published'">
                已分 {{ b.allocated }} / {{ b.coupon_count }} 张 · 已发放 {{ b.delivered }}
              </template>
              <template v-else>{{ b.coupon_count }} 张待分</template>
            </p>
          </div>
        </div>
      </KunCard>
    </div>

    <DevapiStoreBatchCreateModal
      v-model:open="createOpen"
      :range="range"
      @created="handleCreated"
    />
    <DevapiStoreBatchModal
      v-model:open="detailOpen"
      :batch-id="detailId"
      @changed="refresh"
    />
  </div>
</template>
