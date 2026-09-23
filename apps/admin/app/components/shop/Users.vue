<script setup lang="ts">
const api = useApi()
const uuid = ref('')
const view = ref<ShopUserView | null>(null)
const loading = ref(false)
const busy = ref('')

const items = ref<ShopItem[]>([])
const grantItem = ref<number | ''>('')
const grantDays = ref('0')
const grantNote = ref('')

const itemOptions = computed(() =>
  items.value
    .filter((i) => i.status === 'published' || i.status === 'retired')
    .map((i) => ({ value: i.id, label: i.name }))
)

onMounted(async () => {
  const res = await api.get<{ items: ShopItem[] }>('/admin/shop/items')
  items.value = res.data?.items ?? []
})

const lookup = async () => {
  if (!uuid.value.trim()) return
  loading.value = true
  try {
    const res = await api.get<ShopUserView>(`/admin/shop/users/${uuid.value.trim()}`)
    if (res.code !== 0) {
      view.value = null
      useKunMessage(res.message || '查询失败', 'error')
      return
    }
    view.value = res.data
  } finally {
    loading.value = false
  }
}

const run = async (key: string, fn: () => Promise<{ code: number; message: string }>, ok: string) => {
  busy.value = key
  try {
    const res = await fn()
    if (res.code !== 0) {
      useKunMessage(res.message || '操作失败', 'error')
      return
    }
    useKunMessage(ok, 'success')
    await lookup()
  } finally {
    busy.value = ''
  }
}

const grant = () =>
  run(
    'grant',
    () =>
      api.post(`/admin/shop/users/${uuid.value.trim()}/grants`, {
        item_id: grantItem.value,
        duration_days: Number(grantDays.value) || 0,
        note: grantNote.value,
      }),
    '已发放'
  )

const revoke = (e: ShopEntitlement) =>
  run(`revoke:${e.id}`, () => api.post(`/admin/shop/entitlements/${e.id}/revoke`), '已收回')

const refund = (o: ShopOrder) =>
  run(`refund:${o.id}`, () => api.post(`/admin/shop/orders/${o.id}/refund`, { note: '管理员退款' }), '已退款')

const formatDate = (s: string | null) => (s ? new Date(s).toLocaleString('zh-CN') : '永久')
</script>

<template>
  <div class="space-y-4">
    <KunCard class="p-4">
      <div class="flex flex-wrap items-end gap-3">
        <KunInput v-model="uuid" label="用户 UUID" placeholder="在「用户管理」里复制" class="min-w-72 flex-1" @keydown.enter="lookup" />
        <KunButton :loading="loading" @click="lookup">查询</KunButton>
      </div>
    </KunCard>

    <template v-if="view">
      <KunCard class="space-y-3 p-4">
        <h3 class="text-foreground font-semibold">发放物品（不扣萌萌点）</h3>
        <div class="grid gap-3 sm:grid-cols-3">
          <KunSelect v-model="grantItem" label="物品" placeholder="选择物品" :options="itemOptions" />
          <KunInput v-model="grantDays" type="number" label="有效期（天）" description="0 表示永久" />
          <KunInput v-model="grantNote" label="备注" placeholder="例如：活动奖励" />
        </div>
        <div class="flex justify-end">
          <KunButton :disabled="grantItem === ''" :loading="busy === 'grant'" @click="grant">发放</KunButton>
        </div>
      </KunCard>

      <KunCard class="p-4">
        <h3 class="text-foreground mb-3 font-semibold">拥有的物品 · 余额 {{ view.balance }}</h3>
        <p v-if="view.entitlements.length === 0" class="text-default-400 text-sm">没有物品</p>
        <div v-for="e in view.entitlements" :key="e.id" class="border-default-200 flex items-center justify-between gap-3 border-t py-2 text-sm">
          <div>
            <span class="text-foreground">{{ e.item.name }}</span>
            <span class="text-default-400 ml-2 text-xs">
              {{ e.source === 'grant' ? '发放' : '购买' }} · 到期 {{ formatDate(e.expires_at) }}
              {{ e.revoked_at ? '· 已收回' : e.active ? '' : '· 已过期' }}
              {{ e.note ? `· ${e.note}` : '' }}
            </span>
          </div>
          <KunButton v-if="!e.revoked_at" size="sm" variant="flat" color="danger" :loading="busy === `revoke:${e.id}`" @click="revoke(e)">
            收回
          </KunButton>
        </div>
      </KunCard>

      <KunCard class="p-4">
        <h3 class="text-foreground mb-3 font-semibold">订单</h3>
        <p v-if="view.orders.length === 0" class="text-default-400 text-sm">没有订单</p>
        <div v-for="o in view.orders" :key="o.id" class="border-default-200 flex items-center justify-between gap-3 border-t py-2 text-sm">
          <div>
            <span class="text-foreground">#{{ o.id }} {{ o.rewards.map((r) => r.item.name).join(' + ') }}</span>
            <span class="text-default-400 ml-2 text-xs">{{ o.price }} 萌萌点 · {{ formatDate(o.created_at) }}</span>
          </div>
          <KunChip v-if="o.status === 'refunded'" size="sm">已退款</KunChip>
          <KunButton v-else size="sm" variant="flat" color="danger" :loading="busy === `refund:${o.id}`" @click="refund(o)">
            退款
          </KunButton>
        </div>
      </KunCard>
    </template>
  </div>
</template>
