<script setup lang="ts">
const props = defineProps<{ entity: string; label: string; disabled?: boolean }>()
const model = defineModel<string>({ required: true })

const api = useEditorApi()
const query = ref('')
const items = ref<{ id: string; label: string }[]>([])
const chosen = ref('')
const searched = ref(false)

const search = async () => {
  const q = query.value.trim()
  if (!q) {
    items.value = []
    return
  }
  try {
    const page = await api<{ items: { id: string; label: string }[] }>(
      '/v3/catalog/lookup',
      { query: { entity: props.entity, q } }
    )
    items.value = page.items
    searched.value = true
  } catch {
    items.value = []
  }
}

const pick = (item: { id: string; label: string }) => {
  model.value = item.id
  chosen.value = item.label
  items.value = []
  query.value = ''
  searched.value = false
}
</script>

<template>
  <div class="space-y-1">
    <KunInput v-model="model" size="sm" :disabled="props.disabled" :aria-label="props.label" placeholder="编号" />
    <div v-if="!props.disabled" class="flex gap-1">
      <KunInput
        v-model="query"
        size="sm"
        class-name="flex-1"
        placeholder="按名字找"
        :aria-label="`按名字找${props.label}`"
        @keydown.enter.prevent="search"
      />
      <KunButton size="sm" variant="light" @click="search">查找</KunButton>
    </div>
    <p v-if="chosen" class="text-foreground text-sm">{{ chosen }}</p>
    <ul v-if="items.length" class="border-default-200 divide-default-200 divide-y rounded-lg border">
      <li v-for="item in items" :key="item.id">
        <button
          type="button"
          class="hover:bg-default-100 flex w-full justify-between gap-2 px-2 py-1 text-left text-sm"
          @click="pick(item)"
        >
          <span class="text-foreground truncate">{{ item.label }}</span>
          <span class="text-default-400 font-mono text-xs">#{{ item.id }}</span>
        </button>
      </li>
    </ul>
    <p v-else-if="searched" class="text-default-400 text-xs">没有这个名字</p>
  </div>
</template>
