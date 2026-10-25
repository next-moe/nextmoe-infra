<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogWorkCharacter } from '#shared/types/catalog'
import type { WtCharacter } from '#shared/types/walkthrough'
import { wtTone } from '~/constants/walkthrough'
import { keysOf, nextKey, referencesOf, removeCharacter } from '~/utils/walkthrough/doc'

const props = defineProps<{
  roster: CatalogWorkCharacter[]
  rosterLoaded: boolean
}>()

const doc = useWalkthroughDocument()
const chart = doc.chart

const rosterById = computed(() => new Map(props.roster.map((c) => [c.id, c])))
const pinOf = (key: string) => {
  const id = doc.pinByKey.value.get(key)
  return id === undefined ? null : String(id)
}
const pinned = (key: string) => {
  const id = pinOf(key)
  return id ? (rosterById.value.get(id) ?? null) : null
}

const options = computed(() =>
  props.roster.map((c) => ({
    value: c.id,
    label: c.display_name,
    image: c.image?.url ?? null,
    role: c.roster_role
  }))
)

const ROLE_LABEL: Record<string, string> = {
  main: '主要角色',
  secondary: '次要角色',
  appears: '登场',
  unknown: ''
}

const update = (key: string, patch: Partial<WtCharacter>, coalesce?: string) =>
  doc.edit((d) => {
    const c = d.characters.find((x) => x.key === key)
    if (c) Object.assign(c, patch)
  }, coalesce)

const add = async () => {
  const key = nextKey('c', keysOf(chart.value.characters))
  doc.edit((d) => d.characters.push({ key, name: '', aliases: [] }))
  doc.expanded.value = `character:${key}`
  await nextTick()
  document.querySelector<HTMLInputElement>(`[data-wt-character-name="${key}"]`)?.focus()
}

const remove = async (c: WtCharacter) => {
  const refs = referencesOf(chart.value, 'character', c.key)
  const used = refs.sections + refs.endings
  if (used) {
    const ok = await useKunAlert({
      title: `删除角色「${c.name || c.key}」`,
      message: `有 ${refs.endings} 个结局、${refs.sections} 个分节归属于这个角色，删除后它们会变成没有归属。`,
      type: 'warning',
      confirmText: '删除',
      cancelText: '取消'
    })
    if (!ok) return
  }
  doc.edit((d) => removeCharacter(d, c.key))
  doc.setPin(c.key, null)
}

const toggle = (key: string) => {
  const id = `character:${key}`
  doc.expanded.value = doc.expanded.value === id ? null : id
}

const filterRoute = (key: string) => {
  doc.routeFilter.value = doc.routeFilter.value === key ? null : key
}
</script>

<template>
  <section class="space-y-1">
    <div class="flex items-center justify-between px-2">
      <h3 class="text-default-500 text-xs font-medium">角色 · {{ chart.characters.length }}</h3>
      <KunButton v-if="!doc.readonly.value" size="sm" variant="light" is-icon-only aria-label="添加角色" @click="add">
        <KunIcon name="lucide:plus" class="size-4" />
      </KunButton>
    </div>
    <p v-if="!chart.characters.length" class="text-default-400 px-2 text-xs">
      攻略按角色拆分路线。添加角色后，把结局和分节归属到角色上。
    </p>

    <WalkthroughsEditorRailItem
      v-for="(c, i) in chart.characters"
      :key="c.key"
      :title="c.name || '未命名角色'"
      :subtitle="pinned(c.key)?.display_name ?? (pinOf(c.key) ? `角色 #${pinOf(c.key)}` : '未关联角色库')"
      :dot="wtTone(i).dot"
      :issues="doc.issuesOf({ kind: 'character', key: c.key })"
      :expanded="doc.expanded.value === `character:${c.key}`"
      :selected="doc.routeFilter.value === c.key"
      @toggle="toggle(c.key)"
      @remove="remove(c)"
    >
      <template #trailing>
        <img
          v-if="pinned(c.key)?.image?.url"
          :src="pinned(c.key)!.image!.url"
          alt=""
          class="size-6 shrink-0 rounded-full object-cover"
        />
        <span
          v-else-if="!pinOf(c.key)"
          class="bg-warning-100 text-warning-700 shrink-0 rounded px-1 text-[10px] leading-4"
        >
          未关联
        </span>
        <button
          type="button"
          tabindex="-1"
          :title="doc.routeFilter.value === c.key ? '显示全部路线' : '只看这条路线'"
          :class="
            cn(
              'flex size-6 shrink-0 items-center justify-center rounded',
              doc.routeFilter.value === c.key ? 'text-primary-600' : 'text-default-300 hover:text-default-600'
            )
          "
          @click.stop="filterRoute(c.key)"
        >
          <KunIcon name="lucide:filter" class="size-3.5" />
        </button>
      </template>

      <KunInput
        :model-value="c.name"
        size="sm"
        label="名字"
        placeholder="攻略页上的写法"
        :data-wt-character-name="c.key"
        :disabled="doc.readonly.value"
        @update:model-value="update(c.key, { name: String($event) }, `char:${c.key}`)"
        @blur="doc.breakCoalescing"
      />
      <KunTagInput
        :model-value="c.aliases"
        size="sm"
        label="别名"
        placeholder="回车添加"
        :max-tags="32"
        :disabled="doc.readonly.value"
        @update:model-value="update(c.key, { aliases: $event as string[] })"
      />
      <KunSelect
        :model-value="pinOf(c.key)"
        :options="options"
        size="sm"
        label="关联角色库"
        :placeholder="props.rosterLoaded ? '从作品的角色表里选择' : '正在载入角色表…'"
        searchable
        clearable
        :disabled="doc.readonly.value || !props.rosterLoaded"
        description="关联后，攻略页会显示这个角色的立绘和中文名。"
        @update:model-value="doc.setPin(c.key, $event ? Number($event) : null)"
      >
        <template #option="{ option }">
          <img
            v-if="option.image"
            :src="option.image"
            alt=""
            class="size-6 shrink-0 rounded-full object-cover"
          />
          <span v-else class="bg-default-100 size-6 shrink-0 rounded-full" />
          <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
          <span class="text-default-400 shrink-0 text-xs">{{ ROLE_LABEL[option.role] }}</span>
        </template>
      </KunSelect>
    </WalkthroughsEditorRailItem>
  </section>
</template>
