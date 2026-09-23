<script setup lang="ts">
import { SHOP_SLOTS } from '~/constants/shop'

const props = defineProps<{
  items: ShopOwnedItem[]
  loadout: ShopLoadout[]
  userName: string
  avatar: string
}>()

const emit = defineEmits<{ equipped: []; goStore: [] }>()

const api = useApi()
const pending = ref('')

const ownedIn = (slot: ShopKind) =>
  props.items.filter((o) => o.item.kind === slot && o.active)
const wornIn = (slot: ShopKind) =>
  props.loadout.find((l) => l.slot === slot && l.site_id === 0)?.item_id ?? null
const lapsed = computed(() => props.items.filter((o) => !o.active))

const wear = async (slot: ShopKind, itemId: number | null) => {
  pending.value = `${slot}:${itemId ?? 'off'}`
  try {
    const res = await api.put('/shop/me/loadout', {
      slot,
      site_id: 0,
      item_id: itemId
    })
    if (res.code !== 0) {
      useKunMessage(res.message, 'error')
      return
    }
    useKunMessage(itemId ? '已换上，所有站点都会显示' : '已取下', 'success')
    emit('equipped')
  } finally {
    pending.value = ''
  }
}

const formatDate = (s: string) =>
  new Date(s).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric'
  })
</script>

<template>
  <div class="space-y-8">
    <div
      v-if="items.length === 0"
      class="text-default-400 flex flex-col items-center gap-3 py-16 text-sm"
    >
      <KunIcon name="lucide:shirt" class="size-8" />
      你还没有任何装扮
      <KunButton size="sm" variant="flat" @click="emit('goStore')"
        >去商店看看</KunButton
      >
    </div>

    <template v-else>
      <section v-for="s in SHOP_SLOTS" :key="s.slot" class="space-y-3">
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-foreground font-semibold">{{ s.label }}</h2>
          <KunButton
            v-if="wornIn(s.slot) !== null"
            size="sm"
            variant="light"
            color="default"
            :loading="pending === `${s.slot}:off`"
            @click="wear(s.slot, null)"
          >
            {{ s.worn }}
          </KunButton>
        </div>
        <p v-if="ownedIn(s.slot).length === 0" class="text-default-400 text-sm">
          {{ s.empty }}
        </p>
        <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <KunCard
            v-for="owned in ownedIn(s.slot)"
            :key="owned.item.id"
            class="flex flex-col p-5"
          >
            <div class="flex min-h-36 items-center py-4">
              <ShopItemPreview
                :item="owned.item"
                :user-name="userName"
                :avatar="avatar"
                class="w-full"
              />
            </div>
            <h3 class="text-foreground font-semibold">{{ owned.item.name }}</h3>
            <p class="text-default-400 mt-1 text-xs">
              {{
                owned.expires_at
                  ? `${formatDate(owned.expires_at)} 到期`
                  : '永久'
              }}
              · {{ owned.source === 'grant' ? '赠予' : '购买' }}于
              {{ formatDate(owned.acquired_at) }}
            </p>
            <div class="mt-auto flex justify-end pt-4">
              <KunChip
                v-if="wornIn(s.slot) === owned.item.id"
                color="success"
                size="sm"
                >使用中</KunChip
              >
              <KunButton
                v-else
                size="sm"
                :loading="pending === `${s.slot}:${owned.item.id}`"
                @click="wear(s.slot, owned.item.id)"
              >
                换上
              </KunButton>
            </div>
          </KunCard>
        </div>
      </section>
    </template>

    <div v-if="lapsed.length" class="space-y-2">
      <h3 class="text-default-500 text-sm font-medium">已过期</h3>
      <div class="flex flex-wrap gap-2">
        <KunChip v-for="o in lapsed" :key="o.item.id" size="sm">{{
          o.item.name
        }}</KunChip>
      </div>
    </div>
  </div>
</template>
