<script setup lang="ts">
import { SHOP_KIND_LABEL } from '~/constants/shop'

const props = defineProps<{
  offer: ShopOffer
  userName: string
  avatar: string
  balance: number
  owned: boolean
}>()

const emit = defineEmits<{ buy: [] }>()

const lead = computed(() => props.offer.rewards[0])
const title = computed(() =>
  props.offer.rewards.map((r) => r.item.name).join(' + ')
)
const affordable = computed(() => props.balance >= props.offer.price)
const soldOut = computed(
  () => props.offer.stock !== null && props.offer.sold >= props.offer.stock
)
</script>

<template>
  <KunCard class="flex flex-col p-5">
    <div class="flex min-h-36 items-center py-4">
      <ShopItemPreview
        :item="lead?.item"
        :user-name="userName"
        :avatar="avatar"
        class="w-full"
      />
    </div>

    <div class="flex items-start justify-between gap-2">
      <div class="min-w-0">
        <h3 class="text-foreground font-semibold">{{ title }}</h3>
        <p class="text-default-400 text-xs">
          {{ SHOP_KIND_LABEL[lead?.item.kind ?? 'avatar_frame'] }}
        </p>
      </div>
      <KunChip v-if="lead?.duration_days" size="sm" color="info">
        {{ lead.duration_days }} 天
      </KunChip>
    </div>
    <p v-if="lead?.item.description" class="text-default-500 mt-1 text-sm">
      {{ lead.item.description }}
    </p>
    <p v-if="offer.stock !== null" class="text-default-400 mt-1 text-xs">
      限量 {{ offer.stock }} 份，还剩
      {{ Math.max(offer.stock - offer.sold, 0) }} 份
    </p>

    <div class="mt-auto flex items-center justify-between gap-3 pt-4">
      <span class="text-foreground flex items-center gap-1 font-semibold">
        <KunIcon name="lucide:star" class="text-warning size-4" />
        {{ offer.price }}
      </span>
      <KunChip v-if="owned" color="success" size="sm">已拥有</KunChip>
      <KunButton
        v-else
        size="sm"
        :disabled="!affordable || soldOut"
        @click="emit('buy')"
      >
        {{ soldOut ? '已售罄' : affordable ? '购买' : '萌萌点不足' }}
      </KunButton>
    </div>
  </KunCard>
</template>
