<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import {
  SHOP_KIND_SHELF,
  SHOP_SHELVES,
  SHOP_SITE_SHELF_TINT
} from '~/constants/shop'
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'

const api = useApi()
const auth = useAuth()
const user = auth.user

const cdnBase = useRuntimeConfig().public.imageCdnBase as string
const avatarSrc = computed(() =>
  resolveAvatarUrl(user.value ?? null, { cdnBase, variant: '256' }, '')
)

const tab = ref('store')
const tabs: KunTabItem[] = [
  { value: 'store', textValue: '商店' },
  { value: 'wardrobe', textValue: '我的物品' }
]

const offers = ref<ShopOffer[]>([])
const inventory = ref<ShopInventory | null>(null)
const loading = ref(true)

const load = async () => {
  const [catalog, inv] = await Promise.all([
    api.get<{ offers: ShopOffer[] }>('/shop/catalog'),
    api.get<ShopInventory>('/shop/me')
  ])
  if (catalog.code === 0) offers.value = catalog.data.offers ?? []
  if (inv.code === 0) inventory.value = inv.data
  loading.value = false
}

onMounted(load)

const balance = computed(
  () => inventory.value?.balance ?? user.value?.moemoepoint ?? 0
)
const ownedForever = computed(
  () =>
    new Set(
      (inventory.value?.items ?? [])
        .filter((o) => o.active && !o.expires_at)
        .map((o) => o.item.id)
    )
)
const isOwned = (offer: ShopOffer) =>
  offer.rewards.every((r) => ownedForever.value.has(r.item.id))
const usedOf = (offer: ShopOffer) =>
  inventory.value?.limit_used?.[offer.id] ?? 0

const shelves = computed(() => {
  const shared = offers.value.filter((o) => !o.site)
  const byKind = SHOP_SHELVES.map((shelf) => ({
    ...shelf,
    offers: shared.filter(
      (o) =>
        SHOP_KIND_SHELF[o.rewards[0]?.item.kind ?? 'avatar_frame'] === shelf.key
    )
  }))
  const bySite = new Map<number, { site: ShopSite; offers: ShopOffer[] }>()
  for (const o of offers.value) {
    if (!o.site) continue
    const group = bySite.get(o.site.id) ?? { site: o.site, offers: [] }
    group.offers.push(o)
    bySite.set(o.site.id, group)
  }
  const zones = [...bySite.values()].map((g) => ({
    key: `site-${g.site.id}`,
    title: `${g.site.name} 专区`,
    note: `${g.site.domain} 独家，买到后所有站点都能用`,
    icon: 'lucide:store',
    tint: SHOP_SITE_SHELF_TINT,
    offers: g.offers
  }))
  return [...byKind, ...zones].filter((s) => s.offers.length)
})

const buying = ref<ShopOffer | null>(null)
const purchaseOpen = ref(false)
const openPurchase = (offer: ShopOffer) => {
  buying.value = offer
  purchaseOpen.value = true
}

const onPurchased = async () => {
  await Promise.all([load(), auth.fetchUser()])
  tab.value = 'wardrobe'
}

const onEquipped = async () => {
  await Promise.all([load(), auth.fetchUser()])
}
</script>

<template>
  <div class="space-y-8">
    <div class="flex flex-wrap items-end justify-between gap-6">
      <div class="max-w-xl">
        <h1
          class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight"
        >
          萌萌点商店
        </h1>
        <p class="text-default-500 mt-2 text-sm leading-relaxed">
          用萌萌点换装扮、功能和福利。买到的东西属于你的 NextMoe·未萌
          账号，在所有站点通用。
        </p>
      </div>
      <ShopWallet :balance="balance" class="w-full sm:w-auto" />
    </div>

    <KunTab v-model="tab" :items="tabs" />

    <div
      v-if="loading"
      class="grid gap-5 sm:grid-cols-2 lg:grid-cols-3"
      aria-busy="true"
    >
      <KunCard
        v-for="n in 3"
        :key="n"
        padding="none"
        class-name="gap-0 overflow-hidden"
      >
        <KunSkeleton height="13rem" rounded="none" />
        <div class="space-y-3 p-5">
          <KunSkeleton variant="text" width="50%" />
          <KunSkeleton variant="text" />
          <KunSkeleton variant="text" width="30%" />
        </div>
      </KunCard>
    </div>

    <template v-else-if="tab === 'store'">
      <div
        v-if="shelves.length === 0"
        class="text-default-400 flex flex-col items-center gap-3 py-16 text-sm"
      >
        <KunIcon name="lucide:gift" class="size-8" />
        商店还在准备中，过几天再来看看吧
      </div>
      <div v-else class="space-y-12">
        <ShopShelf
          v-for="shelf in shelves"
          :key="shelf.key"
          :title="shelf.title"
          :note="shelf.note"
          :icon="shelf.icon"
          :tint="shelf.tint"
        >
          <ShopOfferCard
            v-for="offer in shelf.offers"
            :key="offer.id"
            :offer="offer"
            :user-name="user?.name ?? ''"
            :avatar="avatarSrc"
            :balance="balance"
            :owned="isOwned(offer)"
            :used="usedOf(offer)"
            @buy="openPurchase(offer)"
          />
        </ShopShelf>
      </div>
    </template>

    <ShopWardrobe
      v-else
      :items="inventory?.items ?? []"
      :loadout="inventory?.loadout ?? []"
      :orders="inventory?.orders ?? []"
      :user-name="user?.name ?? ''"
      :avatar="avatarSrc"
      @equipped="onEquipped"
      @go-store="tab = 'store'"
    />

    <ShopPurchaseModal
      v-model="purchaseOpen"
      :offer="buying"
      :user-name="user?.name ?? ''"
      :avatar="avatarSrc"
      :balance="balance"
      @purchased="onPurchased"
    />
  </div>
</template>
