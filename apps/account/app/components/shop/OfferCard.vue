<script setup lang="ts">
const props = defineProps<{
  offer: ShopOffer
  userName: string
  avatar: string
  balance: number
  owned: boolean
  used: number
}>()

const emit = defineEmits<{ buy: [] }>()

const lead = computed(() => props.offer.rewards[0])
const title = computed(() =>
  props.offer.rewards.map((r) => r.item.name).join(' + ')
)
const isCode = computed(() => lead.value?.item.kind === 'redeem_code')
const unit = computed(() => (isCode.value ? '张' : '份'))

const facts = computed(() => {
  const o = props.offer
  const out: string[] = []
  if (o.stock !== null) out.push(`限量 ${o.stock} 份`)
  if (o.remaining !== null && o.remaining > 0)
    out.push(`还剩 ${o.remaining} ${unit.value}`)
  if (o.per_user_limit) {
    const period = o.limit_period === 'month' ? '每月' : '每人'
    out.push(
      props.used
        ? `${period}限购 ${o.per_user_limit}，已买 ${props.used}`
        : `${period}限购 ${o.per_user_limit}`
    )
  }
  return out
})

const action = computed(() => {
  const o = props.offer
  if (o.remaining === 0) return { label: '已售罄', disabled: true }
  if (o.per_user_limit && props.used >= o.per_user_limit)
    return {
      label: o.limit_period === 'month' ? '本月已买满' : '已达限购',
      disabled: true
    }
  const short = o.price - props.balance
  if (short > 0) return { label: `还差 ${short} 点`, disabled: true }
  return { label: '购买', disabled: false }
})
</script>

<template>
  <KunCard
    padding="none"
    :is-hoverable="true"
    class-name="gap-0 overflow-hidden"
    content-class="gap-0"
  >
    <ShopStage
      :item="lead?.item"
      :user-name="userName"
      :avatar="avatar"
      class-name="h-52"
    >
      <KunChip
        v-if="lead?.duration_days"
        size="sm"
        color="info"
        variant="solid"
        class-name="absolute top-3 right-3"
      >
        {{ lead.duration_days }} 天
      </KunChip>
    </ShopStage>

    <div class="flex flex-1 flex-col p-5">
      <h3 class="text-foreground leading-snug font-semibold">{{ title }}</h3>
      <p
        v-if="lead?.item.description"
        class="text-default-500 mt-1.5 line-clamp-2 text-sm"
      >
        {{ lead.item.description }}
      </p>
      <p v-if="facts.length" class="text-default-400 mt-3 text-xs">
        {{ facts.join(' · ') }}
      </p>

      <div class="mt-auto flex items-center justify-between gap-3 pt-5">
        <ShopPrice :amount="offer.price" class-name="text-xl" />
        <KunChip v-if="owned" color="success" size="md">
          <span class="flex items-center gap-1">
            <KunIcon name="lucide:check" class="size-4" />
            已拥有
          </span>
        </KunChip>
        <KunButton
          v-else
          :color="action.disabled ? 'default' : 'primary'"
          :variant="action.disabled ? 'flat' : 'solid'"
          :disabled="action.disabled"
          @click="emit('buy')"
        >
          {{ action.label }}
        </KunButton>
      </div>
    </div>
  </KunCard>
</template>
