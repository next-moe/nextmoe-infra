<script setup lang="ts">
import { SHOP_OFFER_STATUS } from '~/constants/shop'

const api = useApi()
const offers = ref<ShopOffer[]>([])
const loading = ref(true)
const busy = ref('')

const load = async () => {
  const res = await api.get<{ offers: ShopOffer[] }>('/admin/shop/offers')
  if (res.code === 0) offers.value = res.data.offers ?? []
  else useKunMessage(res.message || '加载失败', 'error')
  loading.value = false
}
onMounted(load)

const modalOpen = ref(false)
const editing = ref<ShopOffer | null>(null)
const openCreate = () => {
  editing.value = null
  modalOpen.value = true
}
const openEdit = (offer: ShopOffer) => {
  editing.value = offer
  modalOpen.value = true
}

const act = async (offer: ShopOffer, action: 'activate' | 'retire') => {
  busy.value = `${offer.id}:${action}`
  try {
    const res = await api.post(`/admin/shop/offers/${offer.id}/${action}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '操作失败', 'error')
      return
    }
    useKunMessage(action === 'activate' ? '已上架' : '已下架', 'success')
    await load()
  } finally {
    busy.value = ''
  }
}

const title = (o: ShopOffer) => o.rewards.map((r) => r.item.name).join(' + ')
const formatDate = (s: string | null) => (s ? new Date(s).toLocaleString('zh-CN') : '不限')
</script>

<template>
  <div class="space-y-4">
    <div class="flex justify-end">
      <KunButton color="primary" @click="openCreate">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建商品
      </KunButton>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="text-primary size-8 animate-spin" />
    </div>
    <KunCard v-else-if="offers.length === 0" class-name="py-12 text-center">
      <p class="text-default-400">还没有商品。先在「物品」里发布头像框，再来这里定价上架</p>
    </KunCard>
    <div v-else class="space-y-2">
      <KunCard v-for="o in offers" :key="o.id" class="p-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="min-w-0 space-y-1">
            <div class="flex items-center gap-2">
              <span class="text-foreground font-semibold">{{ title(o) }}</span>
              <KunChip :color="SHOP_OFFER_STATUS[o.status].color" size="sm">
                {{ SHOP_OFFER_STATUS[o.status].label }}
              </KunChip>
            </div>
            <p class="text-default-500 text-xs">
              #{{ o.id }} · {{ o.price }} 萌萌点
              · {{ o.rewards[0]?.duration_days ? `${o.rewards[0].duration_days} 天` : '永久' }}
              · 已售 {{ o.sold }}{{ o.stock !== null ? ` / ${o.stock}` : '' }}
              · 每人限购 {{ o.per_user_limit || '不限' }}
            </p>
            <p class="text-default-400 text-xs">
              {{ formatDate(o.starts_at) }} — {{ formatDate(o.ends_at) }}
            </p>
          </div>
          <div class="flex gap-2">
            <KunButton
              v-if="o.status !== 'retired'"
              size="sm"
              variant="light"
              color="default"
              @click="openEdit(o)"
            >
              编辑
            </KunButton>
            <KunButton
              v-if="o.status !== 'active'"
              size="sm"
              :loading="busy === `${o.id}:activate`"
              @click="act(o, 'activate')"
            >
              上架
            </KunButton>
            <KunButton
              v-else
              size="sm"
              variant="flat"
              color="danger"
              :loading="busy === `${o.id}:retire`"
              @click="act(o, 'retire')"
            >
              下架
            </KunButton>
          </div>
        </div>
      </KunCard>
    </div>

    <ShopOfferModal v-model="modalOpen" :offer="editing" @saved="load" />
  </div>
</template>
