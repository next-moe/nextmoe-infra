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

watch(open, (v) => {
  if (v) idempotencyKey.value = crypto.randomUUID()
})

const lead = computed(() => props.offer?.rewards[0])
const title = computed(
  () => props.offer?.rewards.map((r) => r.item.name).join(' + ') ?? ''
)
const after = computed(() => props.balance - (props.offer?.price ?? 0))

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
    useKunMessage(`已购买「${title.value}」，去「我的装扮」换上它吧`, 'success')
    open.value = false
    emit('purchased')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" title="确认购买" size="sm">
    <div v-if="offer" class="space-y-5">
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
        <p class="text-default-500 mt-1 text-sm">
          {{
            lead?.duration_days ? `有效期 ${lead.duration_days} 天` : '永久拥有'
          }}
        </p>
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
