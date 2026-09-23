<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'

const tab = ref('items')
const tabs: KunTabItem[] = [
  { value: 'items', textValue: '物品' },
  { value: 'offers', textValue: '商品' },
  { value: 'users', textValue: '用户' },
]
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-foreground text-2xl font-bold">萌萌点商店</h1>
      <p class="text-default-500 mt-1">
        上传头像框素材、发布物品、上架商品，以及给用户发放或退款
      </p>
    </div>
    <KunTab v-model="tab" :items="tabs" />
    <ShopItems v-if="tab === 'items'" />
    <ShopOffers v-else-if="tab === 'offers'" />
    <ShopUsers v-else />
  </div>
</template>
