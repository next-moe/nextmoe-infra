<script setup lang="ts">
const props = defineProps<{
  items: ShopOwnedItem[]
  wornId: number | null
  userName: string
  avatar: string
}>()

const emit = defineEmits<{ equipped: []; goStore: [] }>()

const api = useApi()
const pending = ref<number | 'off' | null>(null)

const frames = computed(() =>
  props.items.filter((o) => o.item.kind === 'avatar_frame' && o.active)
)
const lapsed = computed(() =>
  props.items.filter((o) => o.item.kind === 'avatar_frame' && !o.active)
)

const wear = async (itemId: number | null) => {
  pending.value = itemId ?? 'off'
  try {
    const res = await api.put('/shop/me/loadout', {
      slot: 'avatar_frame',
      site_id: 0,
      item_id: itemId
    })
    if (res.code !== 0) {
      useKunMessage(res.message, 'error')
      return
    }
    useKunMessage(itemId ? '已戴上，所有站点都会显示' : '已摘下头像框', 'success')
    emit('equipped')
  } finally {
    pending.value = null
  }
}

const formatDate = (s: string) =>
  new Date(s).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })
</script>

<template>
  <div class="space-y-6">
    <div
      v-if="frames.length === 0"
      class="text-default-400 flex flex-col items-center gap-3 py-16 text-sm"
    >
      <KunIcon name="lucide:shirt" class="size-8" />
      你还没有头像框
      <KunButton size="sm" variant="flat" @click="emit('goStore')">去商店看看</KunButton>
    </div>

    <template v-else>
      <div class="flex justify-end">
        <KunButton
          size="sm"
          variant="light"
          color="default"
          :disabled="wornId === null"
          :loading="pending === 'off'"
          @click="wear(null)"
        >
          摘下头像框
        </KunButton>
      </div>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <KunCard v-for="owned in frames" :key="owned.item.id" class="flex flex-col p-5">
          <div class="flex justify-center py-6">
            <KunAvatar
              :user="{
                id: 0,
                name: userName,
                avatar,
                avatarDecoration: toAvatarDecoration(owned.item.preview)
              }"
              size="original-sm"
              :is-navigation="false"
            />
          </div>
          <h3 class="text-foreground font-semibold">{{ owned.item.name }}</h3>
          <p class="text-default-400 mt-1 text-xs">
            {{ owned.expires_at ? `${formatDate(owned.expires_at)} 到期` : '永久' }}
            · {{ owned.source === 'grant' ? '赠予' : '购买' }}于 {{ formatDate(owned.acquired_at) }}
          </p>
          <div class="mt-auto flex justify-end pt-4">
            <KunChip v-if="wornId === owned.item.id" color="success" size="sm">佩戴中</KunChip>
            <KunButton
              v-else
              size="sm"
              :loading="pending === owned.item.id"
              @click="wear(owned.item.id)"
            >
              戴上
            </KunButton>
          </div>
        </KunCard>
      </div>
    </template>

    <div v-if="lapsed.length" class="space-y-2">
      <h3 class="text-default-500 text-sm font-medium">已过期</h3>
      <div class="flex flex-wrap gap-2">
        <KunChip v-for="o in lapsed" :key="o.item.id" size="sm">{{ o.item.name }}</KunChip>
      </div>
    </div>
  </div>
</template>
