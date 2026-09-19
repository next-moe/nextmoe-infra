<script setup lang="ts">
import { COUPON_BATCH_STATUS, formatCount } from '~/constants/store'
import type { CouponBatchDetail, CouponGrantInput } from '~~/shared/types/store'

const props = defineProps<{ batchId: number | null }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { required: true })

const api = useApi()
const detail = ref<CouponBatchDetail | null>(null)
const counts = ref<Record<number, Record<number, number>>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const confirm = ref<'publish' | 'delete' | null>(null)
const confirmOpen = ref(false)

const isDraft = computed(() => detail.value?.status === 'draft')

const resetCounts = () => {
  const next: Record<number, Record<number, number>> = {}
  for (const row of detail.value?.split ?? []) {
    next[row.user_id] = Object.fromEntries(row.grants.map((g) => [g.face_value, g.count]))
  }
  counts.value = next
}

const load = async () => {
  if (!props.batchId) return
  loading.value = true
  error.value = ''
  try {
    const res = await api.get<CouponBatchDetail>(`/admin/devapi/store/coupon-batches/${props.batchId}`)
    if (res.code === 0 && res.data) {
      detail.value = res.data
      resetCounts()
    } else {
      error.value = res.message || '读取失败'
    }
  } finally {
    loading.value = false
  }
}

watch(open, (v) => {
  if (v) load()
})

const grants = computed<CouponGrantInput[]>(() =>
  Object.entries(counts.value).flatMap(([userId, byFace]) =>
    Object.entries(byFace)
      .filter(([, n]) => n > 0)
      .map(([face, n]) => ({ user_id: Number(userId), face_value: Number(face), count: n }))
  )
)

const overAllocated = computed(() =>
  (detail.value?.by_value ?? []).some(
    (v) =>
      grants.value
        .filter((g) => g.face_value === v.face_value)
        .reduce((n, g) => n + g.count, 0) > v.count
  )
)

const keptBack = computed(() => {
  const total = detail.value?.coupon_count ?? 0
  return total - grants.value.reduce((n, g) => n + g.count, 0)
})

const ask = (what: 'publish' | 'delete') => {
  confirm.value = what
  confirmOpen.value = true
}

const run = async () => {
  const id = props.batchId
  if (!id || !confirm.value) return
  busy.value = true
  error.value = ''
  try {
    const res =
      confirm.value === 'publish'
        ? await api.post(`/admin/devapi/store/coupon-batches/${id}/publish`, { grants: grants.value })
        : await api.delete(`/admin/devapi/store/coupon-batches/${id}`)
    if (res.code !== 0) {
      error.value = res.message || '操作失败'
      confirmOpen.value = false
      return
    }
    confirmOpen.value = false
    emit('changed')
    if (confirm.value === 'delete') {
      useKunMessage('草稿已删除', 'success')
      open.value = false
    } else {
      useKunMessage('已发布，分到券的用户现在能在开发者平台看到自己的券码', 'success')
      await load()
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="xl" aria-label="优惠券批次">
    <div v-if="loading && !detail" class="flex justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <div v-else-if="detail" class="space-y-4">
      <div>
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-xl font-bold text-foreground">{{ detail.name }}</h2>
          <KunChip :color="COUPON_BATCH_STATUS[detail.status].color" variant="flat" size="sm">
            {{ COUPON_BATCH_STATUS[detail.status].label }}
          </KunChip>
        </div>
        <p class="mt-1 text-sm text-default-500">
          结算区间 {{ detail.period_from }} ~ {{ detail.period_to }} · {{ detail.coupon_count }} 张 ·
          {{ formatCount(detail.points) }} 点
          <template v-if="detail.published_at">
            · 发布于 {{ new Date(detail.published_at).toLocaleString('zh-CN') }}
          </template>
        </p>
        <p v-if="detail.note" class="mt-1 text-sm text-default-400">{{ detail.note }}</p>
      </div>

      <div v-if="isDraft" class="rounded-lg bg-primary-50 p-3 text-sm text-primary-700">
        下表是按去重点击占比算出的建议：同一用户名下参与分成的应用合并计算，应得点数向下取整；从大面额开始，每张券给剩余应得最多、且放得下这张券的用户，放不下的券留在平台。可以直接改张数；没分出去的券不会给任何人。
      </div>

      <DevapiStoreBatchSplit
        v-model="counts"
        :rows="detail.split"
        :excluded="detail.excluded"
        :values="detail.by_value"
        :readonly="!isDraft"
      />

      <template v-if="!isDraft">
        <h3 class="text-base font-semibold text-foreground">券码</h3>
        <DevapiStoreBatchCoupons :coupons="detail.coupon_list" />
      </template>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">{{ error }}</div>

      <div v-if="isDraft" class="flex flex-wrap items-center justify-between gap-3">
        <KunButton color="danger" variant="flat" :disabled="busy" @click="ask('delete')">
          删除草稿
        </KunButton>
        <div class="flex flex-wrap items-center gap-3">
          <span v-if="keptBack > 0" class="text-sm text-default-500">{{ keptBack }} 张留在平台</span>
          <KunButton variant="flat" :disabled="busy" @click="resetCounts">按占比重算</KunButton>
          <KunButton color="primary" :disabled="busy || overAllocated" @click="ask('publish')">
            发布
          </KunButton>
        </div>
      </div>
    </div>

    <div v-else-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">{{ error }}</div>
  </KunModal>

  <KunModal v-model="confirmOpen" size="sm" role="alertdialog" aria-label="确认">
    <div class="space-y-4">
      <h3 class="text-lg font-bold text-foreground">
        {{ confirm === 'publish' ? '发布这批券？' : '删除这份草稿？' }}
      </h3>
      <p class="text-sm text-default-500">
        <template v-if="confirm === 'publish'">
          发布后分到券的用户立刻能在开发者平台看到自己的券码，分配不能再改。
        </template>
        <template v-else>草稿和其中的券码都会删除，之后可以重新录入同样的券码。</template>
      </p>
      <div class="flex justify-end gap-3">
        <KunButton variant="flat" @click="confirmOpen = false">取消</KunButton>
        <KunButton
          :color="confirm === 'publish' ? 'primary' : 'danger'"
          :disabled="busy"
          @click="run"
        >
          <KunIcon v-if="busy" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          确认
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
