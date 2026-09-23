<script setup lang="ts">
const props = defineProps<{ offer: ShopOffer | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi()
const items = ref<ShopItem[]>([])
const itemId = ref<number | ''>('')
const price = ref('100')
const durationDays = ref('0')
const stock = ref('')
const perUserLimit = ref('0')
const sortOrder = ref('0')
const startsAt = ref('')
const endsAt = ref('')
const saving = ref(false)
const error = ref('')

const toLocalInput = (s: string | null) => {
  if (!s) return ''
  const d = new Date(s)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

watch(open, async (v) => {
  if (!v) return
  error.value = ''
  const res = await api.get<{ items: ShopItem[] }>('/admin/shop/items')
  items.value = (res.data?.items ?? []).filter((i) => i.status !== 'retired')
  const o = props.offer
  itemId.value = o?.rewards[0]?.item.id ?? ''
  price.value = String(o?.price ?? 100)
  durationDays.value = String(o?.rewards[0]?.duration_days ?? 0)
  stock.value = o?.stock !== null && o?.stock !== undefined ? String(o.stock) : ''
  perUserLimit.value = String(o?.per_user_limit ?? 0)
  sortOrder.value = String(o?.sort_order ?? 0)
  startsAt.value = toLocalInput(o?.starts_at ?? null)
  endsAt.value = toLocalInput(o?.ends_at ?? null)
})

const itemOptions = computed(() =>
  items.value.map((i) => ({
    value: i.id,
    label: `${i.name}（${i.status === 'published' ? '已发布' : '未发布'}）`,
  }))
)

const save = async () => {
  error.value = ''
  if (itemId.value === '') {
    error.value = '请选择物品'
    return
  }
  saving.value = true
  try {
    const days = Number(durationDays.value) || 0
    const body = {
      price: Number(price.value),
      rewards: [{ item_id: itemId.value, ...(days > 0 ? { duration_days: days } : {}) }],
      stock: stock.value === '' ? null : Number(stock.value),
      per_user_limit: Number(perUserLimit.value) || 0,
      sort_order: Number(sortOrder.value) || 0,
      starts_at: startsAt.value ? new Date(startsAt.value).toISOString() : null,
      ends_at: endsAt.value ? new Date(endsAt.value).toISOString() : null,
    }
    const res = props.offer
      ? await api.put(`/admin/shop/offers/${props.offer.id}`, body)
      : await api.post('/admin/shop/offers', body)
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.offer ? '已保存' : '已创建，上架后用户才能看到', 'success')
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" :title="offer ? '编辑商品' : '新建商品'" size="md">
    <div class="space-y-4">
      <KunSelect v-model="itemId" label="物品" placeholder="选择一个头像框" :options="itemOptions" />
      <div class="grid grid-cols-2 gap-3">
        <KunInput v-model="price" type="number" label="价格（萌萌点）" description="不能低于商店最低价（默认 100）" />
        <KunInput v-model="durationDays" type="number" label="有效期（天）" description="0 表示永久" />
        <KunInput v-model="stock" type="number" label="库存" description="留空表示不限量" />
        <KunInput v-model="perUserLimit" type="number" label="每人限购" description="0 表示不限" />
        <KunInput v-model="startsAt" type="datetime-local" label="开始时间" description="留空表示立即" />
        <KunInput v-model="endsAt" type="datetime-local" label="结束时间" description="留空表示长期" />
        <KunInput v-model="sortOrder" type="number" label="排序" description="越大越靠前" />
      </div>
      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">{{ error }}</div>
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">取消</KunButton>
        <KunButton color="primary" :loading="saving" @click="save">保存</KunButton>
      </div>
    </div>
  </KunModal>
</template>
