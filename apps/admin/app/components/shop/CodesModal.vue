<script setup lang="ts">
const props = defineProps<{ item: ShopItem | null }>()
const open = defineModel<boolean>({ required: true })

const api = useApi()
const pool = ref<ShopCodePool | null>(null)
const pasted = ref('')
const expiresOn = ref('')
const adding = ref(false)
const busy = ref(0)

const load = async () => {
  if (!props.item) return
  const res = await api.get<ShopCodePool>(
    `/admin/shop/codes?item_id=${props.item.id}`
  )
  if (res.code !== 0) {
    useKunMessage(res.message || '加载失败', 'error')
    return
  }
  pool.value = res.data
}

watch(open, (v) => {
  if (!v) return
  pool.value = null
  pasted.value = ''
  expiresOn.value = ''
  load()
})

const parsed = computed(() =>
  pasted.value
    .split(/[\s,，]+/)
    .map((c) => c.trim())
    .filter(Boolean)
)

const cutoff = computed(() => {
  const days = pool.value?.shelf_days ?? 0
  return new Date(Date.now() + 9 * 3600e3 + days * 86400e3)
    .toISOString()
    .slice(0, 10)
})

const stateOf = (c: ShopCode) => {
  if (c.order_id)
    return { label: `已售 · 用户 #${c.user_id}`, color: 'default' as const }
  if (c.expires_on && c.expires_on < cutoff.value)
    return { label: '已过期或临期，不再出售', color: 'danger' as const }
  return { label: '可售', color: 'success' as const }
}

const add = async () => {
  if (!props.item || parsed.value.length === 0) return
  adding.value = true
  try {
    const res = await api.post<{ added: number; duplicates: string[] }>(
      '/admin/shop/codes',
      {
        item_id: props.item.id,
        codes: parsed.value,
        expires_on: expiresOn.value || null
      }
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '添加失败', 'error')
      return
    }
    const dup = res.data.duplicates.length
    useKunMessage(
      `已添加 ${res.data.added} 个${dup ? `，${dup} 个已经在码池里了` : ''}`,
      dup ? 'warn' : 'success'
    )
    pasted.value = ''
    await load()
  } finally {
    adding.value = false
  }
}

const remove = async (c: ShopCode) => {
  busy.value = c.id
  try {
    const res = await api.delete(`/admin/shop/codes/${c.id}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    await load()
  } finally {
    busy.value = 0
  }
}
</script>

<template>
  <KunModal v-model="open" :title="`码池 · ${item?.name ?? ''}`" size="lg">
    <div class="space-y-4">
      <div v-if="pool" class="grid grid-cols-3 gap-3 text-center">
        <KunCard class="p-3">
          <p class="text-foreground text-xl font-semibold">
            {{ pool.sellable }}
          </p>
          <p class="text-default-400 text-xs">可售</p>
        </KunCard>
        <KunCard class="p-3">
          <p class="text-foreground text-xl font-semibold">{{ pool.sold }}</p>
          <p class="text-default-400 text-xs">已售</p>
        </KunCard>
        <KunCard class="p-3">
          <p class="text-foreground text-xl font-semibold">{{ pool.total }}</p>
          <p class="text-default-400 text-xs">总数</p>
        </KunCard>
      </div>

      <div class="border-default-200 space-y-3 rounded-lg border p-4">
        <KunTextarea
          v-model="pasted"
          label="添加兑换码"
          placeholder="每行一个，也可以用空格或逗号分隔"
          :rows="5"
          :description="`识别到 ${parsed.length} 个码；重复的会自动跳过`"
        />
        <div class="flex flex-wrap items-end gap-3">
          <KunInput
            v-model="expiresOn"
            type="date"
            label="最后可用日（日本时间）"
            :description="`留空表示不过期；离过期不足 ${pool?.shelf_days ?? 3} 天的码不再出售`"
            class="min-w-48 flex-1"
          />
          <KunButton
            color="primary"
            :loading="adding"
            :disabled="parsed.length === 0"
            @click="add"
          >
            添加 {{ parsed.length || '' }} 个
          </KunButton>
        </div>
      </div>

      <div v-if="pool?.codes.length" class="max-h-96 overflow-y-auto">
        <div
          v-for="c in pool.codes"
          :key="c.id"
          class="border-default-200 flex items-center justify-between gap-3 border-t py-2 text-sm"
        >
          <div class="min-w-0">
            <p class="text-foreground truncate font-mono">{{ c.code }}</p>
            <p class="text-default-400 text-xs">
              {{ c.expires_on ? `${c.expires_on} 前有效` : '不过期' }}
            </p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <KunChip :color="stateOf(c).color" size="sm">
              {{ stateOf(c).label }}
            </KunChip>
            <KunButton
              v-if="!c.order_id"
              size="sm"
              variant="light"
              color="danger"
              :loading="busy === c.id"
              @click="remove(c)"
            >
              删除
            </KunButton>
          </div>
        </div>
      </div>
      <p v-else-if="pool" class="text-default-400 text-center text-sm">
        码池还是空的
      </p>
    </div>
  </KunModal>
</template>
