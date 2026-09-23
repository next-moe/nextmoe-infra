<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
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
  { value: 'wardrobe', textValue: '我的装扮' }
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
const sections = computed(() => {
  const global = offers.value.filter((o) => !o.site)
  const bySite = new Map<number, { site: ShopSite; offers: ShopOffer[] }>()
  for (const o of offers.value) {
    if (!o.site) continue
    const group = bySite.get(o.site.id) ?? { site: o.site, offers: [] }
    group.offers.push(o)
    bySite.set(o.site.id, group)
  }
  return [
    ...(global.length
      ? [{ key: 'all', title: '全站', note: '', offers: global }]
      : []),
    ...[...bySite.values()].map((g) => ({
      key: `site-${g.site.id}`,
      title: `${g.site.name} 专区`,
      note: `${g.site.domain} 独家`,
      offers: g.offers
    }))
  ]
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
  <div class="space-y-6">
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1
          class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight"
        >
          萌萌点商店
        </h1>
        <p class="text-default-500 mt-2 text-sm">
          用萌萌点换头像框和主页背景，装扮在所有 NextMoe·未萌 站点通用
        </p>
      </div>
      <KunChip color="warning" variant="flat" size="md">
        <span class="flex items-center gap-1.5">
          <KunIcon name="lucide:star" class="size-4" />
          {{ balance }} 萌萌点
        </span>
      </KunChip>
    </div>

    <KunTab v-model="tab" :items="tabs" />

    <div v-if="loading" class="flex justify-center py-16">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-6 animate-spin"
      />
    </div>

    <template v-else-if="tab === 'store'">
      <div
        v-if="offers.length === 0"
        class="text-default-400 flex flex-col items-center gap-3 py-16 text-sm"
      >
        <KunIcon name="lucide:gift" class="size-8" />
        商店还在准备中，过几天再来看看吧
      </div>
      <div v-else class="space-y-8">
        <section v-for="sec in sections" :key="sec.key" class="space-y-3">
          <div v-if="sections.length > 1" class="flex items-baseline gap-2">
            <h2 class="text-foreground font-semibold">{{ sec.title }}</h2>
            <span v-if="sec.note" class="text-default-400 text-xs">{{
              sec.note
            }}</span>
          </div>
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <ShopOfferCard
              v-for="offer in sec.offers"
              :key="offer.id"
              :offer="offer"
              :user-name="user?.name ?? ''"
              :avatar="avatarSrc"
              :balance="balance"
              :owned="offer.rewards.every((r) => ownedForever.has(r.item.id))"
              @buy="openPurchase(offer)"
            />
          </div>
        </section>
      </div>
    </template>

    <ShopWardrobe
      v-else
      :items="inventory?.items ?? []"
      :loadout="inventory?.loadout ?? []"
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
