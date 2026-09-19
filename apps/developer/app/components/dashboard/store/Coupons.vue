<script setup lang="ts">
import type { StoreCoupon, StoreCoupons } from '~~/shared/types/store'

const api = useApi()
const { data, refresh } = await useApiFetch<StoreCoupons>('/dev/store/coupons')

const coupons = computed(() => data.value?.coupons ?? [])
const shares = computed(() => data.value?.shares ?? [])
const saving = ref<number | null>(null)

const batches = computed(() =>
  shares.value.map((share) => ({
    share,
    coupons: coupons.value.filter((c) => c.batch_id === share.batch_id)
  }))
)

const fmt = (n: number) => n.toLocaleString()
const pct = (ppm: number) => `${(ppm / 10_000).toFixed(2)}%`

const toggleDelivered = async (c: StoreCoupon) => {
  saving.value = c.id
  try {
    const res = await api.post(`/dev/store/coupons/${c.id}/delivered`, {
      delivered: !c.delivered_at
    })
    if (res.code === 0) {
      await refresh()
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    saving.value = null
  }
}
</script>

<template>
  <div v-if="batches.length" class="space-y-3">
    <div>
      <h2 class="text-foreground text-lg font-semibold">DLsite 优惠券</h2>
      <p class="text-default-500 mt-1 text-sm">
        DLsite 按生态整体的分销销售返还优惠券,平台按结算区间里的去重点击占比分发:你名下参与分成的应用合并计算,应得点数向下取整,凑不够一张券的部分不分。券码只有你能看到,发给你的用户之后可以标记为已发放。
      </p>
    </div>

    <KunCard
      v-for="{ share, coupons: list } in batches"
      :key="share.batch_id"
      content-class="justify-start gap-4 items-stretch"
      class-name="p-6"
    >
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p class="text-foreground font-semibold">{{ share.batch_name }}</p>
          <p class="text-default-500 mt-0.5 text-sm">
            结算区间 {{ share.period_from }} ~ {{ share.period_to }}
            <template v-if="share.apps.length">
              · {{ share.apps.map((a) => a.name).join('、') }}
            </template>
          </p>
        </div>
        <div class="text-right text-sm">
          <p class="text-foreground font-semibold">分到 {{ fmt(share.allocated_points) }} 点</p>
          <p class="text-default-400">
            去重点击 {{ fmt(share.uniques) }} · 占比 {{ pct(share.share_ppm) }} · 按比例应得
            {{ fmt(share.entitled_points) }} 点
          </p>
        </div>
      </div>

      <p v-if="!list.length" class="text-default-400 text-sm">
        这一批按比例折算不到一张券。
      </p>
      <div v-else class="divide-default-200 divide-y">
        <div
          v-for="c in list"
          :key="c.id"
          class="flex flex-wrap items-center justify-between gap-3 py-2"
        >
          <div class="flex min-w-0 items-center gap-3">
            <KunChip color="primary" variant="flat" size="sm">
              {{ fmt(c.face_value) }} 点
            </KunChip>
            <KunCopy :text="c.code" size="sm" />
            <span v-if="c.expires_on" class="text-default-400 text-xs">
              有效期至 {{ c.expires_on }}
            </span>
          </div>
          <KunButton
            size="sm"
            :variant="c.delivered_at ? 'flat' : 'bordered'"
            :color="c.delivered_at ? 'success' : 'default'"
            :disabled="saving === c.id"
            @click="toggleDelivered(c)"
          >
            <KunIcon
              :name="c.delivered_at ? 'lucide:check' : 'lucide:send'"
              class="mr-1 size-4"
            />
            {{ c.delivered_at ? '已发放' : '标记为已发放' }}
          </KunButton>
        </div>
      </div>
    </KunCard>
  </div>
</template>
