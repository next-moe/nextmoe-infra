<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = defineProps<{
  offer: ShopOffer | null
  userName: string
  avatar: string
  balance: number
}>()

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ purchased: [] }>()

const api = useApi()
const submitting = ref(false)
const idempotencyKey = ref('')
const codes = ref<ShopCode[]>([])

watch(open, (v) => {
  if (v) {
    idempotencyKey.value = crypto.randomUUID()
    codes.value = []
  } else if (codes.value.length) {
    emit('purchased')
  }
})

const lead = computed(() => props.offer?.rewards[0])
const title = computed(
  () => props.offer?.rewards.map((r) => r.item.name).join(' + ') ?? ''
)
const isCode = computed(() => lead.value?.item.kind === 'redeem_code')
const after = computed(() => props.balance - (props.offer?.price ?? 0))
const term = computed(() => {
  if (isCode.value) return '付款后立即发放一张兑换码'
  return lead.value?.duration_days
    ? `有效期 ${lead.value.duration_days} 天`
    : '永久拥有'
})
const nextStep: Partial<Record<ShopKind, string>> = {
  avatar_frame: '去「我的物品」换上它吧',
  profile_background: '去「我的物品」换上它吧',
  profile_about: '去「个人资料」写你的主页介绍吧'
}

const confirm = async () => {
  if (!props.offer) return
  submitting.value = true
  try {
    const res = await api.post<ShopPurchased>('/shop/orders', {
      offer_id: props.offer.id,
      idempotency_key: idempotencyKey.value
    })
    if (res.code !== 0) {
      useKunMessage(res.message, 'error')
      return
    }
    const got = res.data.order.codes ?? []
    if (got.length) {
      codes.value = got
      return
    }
    const hint = nextStep[lead.value?.item.kind ?? 'avatar_frame'] ?? ''
    useKunMessage(`已购买「${title.value}」，${hint}`, 'success')
    open.value = false
    emit('purchased')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="codes.length ? '购买成功' : '确认购买'"
    size="sm"
  >
    <div v-if="codes.length" class="space-y-5">
      <p class="text-default-500 text-sm">
        这是你的「{{ title }}」兑换码，之后也能在「我的物品」里找到它。
      </p>
      <div
        v-for="c in codes"
        :key="c.code"
        class="border-warning-300 bg-warning-50 flex flex-col items-center gap-2 rounded-xl border border-dashed p-5"
      >
        <KunCopy :text="c.code" variant="flat" class-name="font-mono" />
        <span v-if="c.expires_on" class="text-default-500 text-xs">
          {{ c.expires_on }} 前有效
        </span>
      </div>
      <p v-if="lead?.item.description" class="text-default-500 text-sm">
        {{ lead.item.description }}
      </p>
      <div class="flex justify-end">
        <KunButton @click="open = false">好的</KunButton>
      </div>
    </div>

    <div v-else-if="offer" class="space-y-5">
      <ShopStage
        :item="lead?.item"
        :user-name="userName"
        :avatar="avatar"
        decoration="always"
        class-name="h-52 rounded-xl"
      />
      <div>
        <p class="text-foreground text-lg font-semibold">{{ title }}</p>
        <p class="text-default-500 mt-1 text-sm">{{ term }}</p>
      </div>

      <div class="bg-default-100 space-y-2.5 rounded-xl p-4 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-default-500">价格</span>
          <ShopPrice :amount="offer.price" />
        </div>
        <div class="flex items-center justify-between">
          <span class="text-default-500">当前余额</span>
          <span class="text-foreground tabular-nums">{{ balance }}</span>
        </div>
        <div
          class="border-default-200 flex items-center justify-between border-t pt-2.5"
        >
          <span class="text-default-500">购买后余额</span>
          <span
            :class="
              cn(
                'font-semibold tabular-nums',
                after < 0 ? 'text-danger-600' : 'text-foreground'
              )
            "
          >
            {{ after }}
          </span>
        </div>
      </div>

      <p
        v-if="isCode"
        class="text-default-500 flex items-start gap-1.5 text-xs leading-relaxed"
      >
        <KunIcon name="lucide:info" class="mt-0.5 size-3.5 shrink-0" />
        兑换码发出后无法收回，所以购买后不能退款。
      </p>

      <div class="flex justify-end gap-2">
        <KunButton variant="light" color="default" @click="open = false">
          取消
        </KunButton>
        <KunButton :loading="submitting" :disabled="after < 0" @click="confirm">
          确认购买
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
