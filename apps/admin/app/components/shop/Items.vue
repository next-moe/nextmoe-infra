<script setup lang="ts">
import { SHOP_ITEM_ACTIONS, SHOP_ITEM_STATUS } from '~/constants/shop'

const api = useApi()
const items = ref<ShopItem[]>([])
const loading = ref(true)
const busy = ref('')

const load = async () => {
  const res = await api.get<{ items: ShopItem[] }>('/admin/shop/items')
  if (res.code === 0) items.value = res.data.items ?? []
  else useKunMessage(res.message || '加载失败', 'error')
  loading.value = false
}
onMounted(load)

const modalOpen = ref(false)
const editing = ref<ShopItem | null>(null)
const openCreate = () => {
  editing.value = null
  modalOpen.value = true
}
const openEdit = (item: ShopItem) => {
  editing.value = item
  modalOpen.value = true
}

const act = async (item: ShopItem, action: string, label: string) => {
  busy.value = `${item.id}:${action}`
  try {
    const res = await api.post(`/admin/shop/items/${item.id}/${action}`)
    if (res.code !== 0) {
      useKunMessage(res.message || `${label}失败`, 'error')
      return
    }
    useKunMessage(`「${item.name}」已${label}`, 'success')
    await load()
  } finally {
    busy.value = ''
  }
}

const remove = async (item: ShopItem) => {
  const res = await api.delete(`/admin/shop/items/${item.id}`)
  if (res.code !== 0) {
    useKunMessage(res.message || '删除失败', 'error')
    return
  }
  useKunMessage('已删除', 'success')
  await load()
}

const sample = (item: ShopItem) => ({
  id: 0,
  name: '预览',
  avatar: '',
  avatarDecoration: item.preview
    ? { src: item.preview.static_url, animatedSrc: item.preview.animated_url }
    : null,
})
</script>

<template>
  <div class="space-y-4">
    <div class="flex justify-end">
      <KunButton color="primary" @click="openCreate">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建头像框
      </KunButton>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="text-primary size-8 animate-spin" />
    </div>
    <KunCard v-else-if="items.length === 0" class-name="py-12 text-center">
      <p class="text-default-400">还没有物品，先上传一个头像框吧</p>
    </KunCard>
    <div v-else class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <KunCard v-for="item in items" :key="item.id" class="flex flex-col p-5">
        <div class="flex justify-center py-5">
          <KunAvatar :user="sample(item)" size="original-sm" :is-navigation="false" />
        </div>
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0">
            <h3 class="text-foreground truncate font-semibold">{{ item.name }}</h3>
            <p class="text-default-400 text-xs">#{{ item.id }} · 头像框</p>
          </div>
          <KunChip :color="SHOP_ITEM_STATUS[item.status].color" size="sm">
            {{ SHOP_ITEM_STATUS[item.status].label }}
          </KunChip>
        </div>
        <p v-if="item.description" class="text-default-500 mt-2 text-sm">
          {{ item.description }}
        </p>
        <div class="mt-auto flex flex-wrap justify-end gap-2 pt-4">
          <KunButton size="sm" variant="light" color="default" @click="openEdit(item)">
            编辑
          </KunButton>
          <KunButton
            v-if="item.status === 'draft' && !item.published_at"
            size="sm"
            variant="light"
            color="danger"
            @click="remove(item)"
          >
            删除
          </KunButton>
          <KunButton
            v-for="a in SHOP_ITEM_ACTIONS[item.status]"
            :key="a.action"
            size="sm"
            :variant="a.action === 'publish' || a.action === 'relist' ? 'solid' : 'flat'"
            :color="a.action === 'retire' || a.action === 'reject' ? 'danger' : 'primary'"
            :loading="busy === `${item.id}:${a.action}`"
            @click="act(item, a.action, a.label)"
          >
            {{ a.label }}
          </KunButton>
        </div>
      </KunCard>
    </div>

    <ShopItemModal v-model="modalOpen" :item="editing" @saved="load" />
  </div>
</template>
