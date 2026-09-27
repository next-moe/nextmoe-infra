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
const after = computed(() => props.balance - (props.offer?.price ?? 0))
const term = computed(() => {
  if (lead.value?.item.kind === 'redeem_code') return '购买后立即得到一张兑换码'
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
        class="border-default-200 flex flex-col items-center gap-2 rounded-lg border p-4"
      >
        <KunCopy :text="c.code" variant="flat" class-name="font-mono" />
        <span v-if="c.expires_on" class="text-default-400 text-xs">
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
      <div class="py-4">
        <ShopItemPreview
          :item="lead?.item"
          :user-name="userName"
          :avatar="avatar"
          decoration="always"
        />
      </div>
      <div class="text-center">
        <p class="text-foreground font-semibold">{{ title }}</p>
        <p class="text-default-500 mt-1 text-sm">{{ term }}</p>
      </div>
      <div class="border-default-200 space-y-2 rounded-lg border p-4 text-sm">
        <div class="flex justify-between">
          <span class="text-default-500">价格</span>
          <span class="text-foreground">{{ offer.price }} 萌萌点</span>
        </div>
        <div class="flex justify-between">
          <span class="text-default-500">购买后余额</span>
          <span :class="cn(after < 0 ? 'text-danger' : 'text-foreground')">
            {{ after }} 萌萌点
          </span>
        </div>
      </div>
      <div class="flex justify-end gap-2">
        <KunButton variant="light" color="default" @click="open = false"
          >取消</KunButton
        >
        <KunButton :loading="submitting" :disabled="after < 0" @click="confirm">
          确认购买
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
