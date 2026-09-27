<script setup lang="ts">
import { SHOP_KIND_ICON, SHOP_SLOTS } from '~/constants/shop'

const props = defineProps<{
  items: ShopOwnedItem[]
  loadout: ShopLoadout[]
  orders: ShopOrder[]
  userName: string
  avatar: string
}>()

const emit = defineEmits<{ equipped: []; goStore: [] }>()

const api = useApi()
const pending = ref('')

const ownedIn = (slot: ShopSlot) =>
  props.items.filter((o) => o.item.kind === slot && o.active)
const wornIn = (slot: ShopSlot) =>
  props.loadout.find((l) => l.slot === slot && l.site_id === 0)?.item_id ?? null
const lapsed = computed(() => props.items.filter((o) => !o.active))
const perks = computed(() =>
  props.items.filter((o) => o.active && o.item.kind === 'profile_about')
)
const codes = computed(() =>
  props.orders
    .filter((o) => o.status === 'completed')
    .flatMap((o) =>
      (o.codes ?? []).map((c) => ({
        ...c,
        name:
          o.rewards.find((r) => r.item.id === c.item_id)?.item.name ?? '兑换码',
        boughtAt: o.created_at
      }))
    )
)

const wear = async (slot: ShopSlot, itemId: number | null) => {
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
      v-if="items.length === 0 && codes.length === 0"
      class="text-default-400 flex flex-col items-center gap-3 py-16 text-sm"
    >
      <KunIcon name="lucide:shirt" class="size-8" />
      你还没有任何物品
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
            padding="none"
            class-name="gap-0 overflow-hidden"
            content-class="gap-0"
          >
            <ShopStage
              :item="owned.item"
              :user-name="userName"
              :avatar="avatar"
              class-name="h-44"
            >
              <KunChip
                v-if="wornIn(s.slot) === owned.item.id"
                color="success"
                variant="solid"
                size="sm"
                class-name="absolute top-3 right-3"
              >
                使用中
              </KunChip>
            </ShopStage>
            <div class="flex flex-1 items-end justify-between gap-3 p-5">
              <div class="min-w-0">
                <h3 class="text-foreground font-semibold">
                  {{ owned.item.name }}
                </h3>
                <p class="text-default-400 mt-1 text-xs">
                  {{
                    owned.expires_at
                      ? `${formatDate(owned.expires_at)} 到期`
                      : '永久'
                  }}
                  · {{ owned.source === 'grant' ? '赠予' : '购买' }}于
                  {{ formatDate(owned.acquired_at) }}
                </p>
              </div>
              <KunButton
                v-if="wornIn(s.slot) !== owned.item.id"
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

    <section v-if="perks.length" class="space-y-3">
      <h2 class="text-foreground font-semibold">功能</h2>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <KunCard
          v-for="owned in perks"
          :key="owned.item.id"
          class="p-5"
          content-class="flex-row items-center justify-start gap-4"
        >
          <div
            class="bg-primary-50 text-primary-600 flex size-12 shrink-0 items-center justify-center rounded-xl"
          >
            <KunIcon :name="SHOP_KIND_ICON.profile_about!" class="size-6" />
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="text-foreground font-semibold">{{ owned.item.name }}</h3>
            <p class="text-default-400 text-xs">
              {{
                owned.expires_at
                  ? `${formatDate(owned.expires_at)} 到期`
                  : '永久'
              }}
            </p>
          </div>
          <KunButton href="/profile" size="sm" variant="flat"
            >去写介绍</KunButton
          >
        </KunCard>
      </div>
    </section>

    <section v-if="codes.length" class="space-y-3">
      <h2 class="text-foreground font-semibold">我的兑换码</h2>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <KunCard
          v-for="c in codes"
          :key="c.code"
          padding="md"
          class-name="border-warning-300 border-dashed"
        >
          <div class="flex items-center gap-2">
            <div
              class="bg-warning-100 text-warning-600 flex size-8 shrink-0 items-center justify-center rounded-lg"
            >
              <KunIcon :name="SHOP_KIND_ICON.redeem_code!" class="size-4" />
            </div>
            <h3 class="text-foreground truncate font-semibold">{{ c.name }}</h3>
          </div>
          <KunCopy :text="c.code" variant="flat" class-name="font-mono" />
          <p class="text-default-400 text-xs">
            {{ formatDate(c.boughtAt) }} 购买
            <template v-if="c.expires_on">
              · {{ c.expires_on }} 前有效
            </template>
          </p>
        </KunCard>
      </div>
    </section>

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
