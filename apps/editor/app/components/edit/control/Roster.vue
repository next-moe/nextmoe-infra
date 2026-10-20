<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaMember } from '#shared/types/edit'

const props = defineProps<{ fieldKey: string }>()

const {
  context,
  value,
  baseValue,
  disabled,
  field,
  companion,
  suppressed,
  toggleSuppressed
} = useEditorField(() => props.fieldKey)
const refs = context.refs

type Row = { character_id: number; kind: number; spoiler: number }

const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseByCharacter = computed(
  () =>
    new Map(
      (Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []).map(
        (row) => [String(row.character_id), row]
      )
    )
)
const memberOf = (key: string) =>
  field.value?.element?.members?.find((m) => m.key === key) as
    CatalogSchemaMember | undefined

const filter = ref('')
const visibleRows = computed(() => {
  const q = filter.value.trim()
  return rows.value
    .map((row, index) => ({ row, index }))
    .filter(
      ({ row }) =>
        !q ||
        refs.nameOf('character', row.character_id).includes(q) ||
        String(row.character_id) === q
    )
})

const setMember = (index: number, key: 'kind' | 'spoiler', next: unknown) => {
  value.value = rows.value.map((row, i) =>
    i === index ? { ...row, [key]: Number(next) } : row
  )
}

const identityOf = (row: Row) =>
  context.rosterIdentity.value[String(row.character_id)]
const isHidden = (row: Row) => {
  const identity = identityOf(row)
  return Boolean(identity && suppressed.value.includes(identity))
}
const changed = (row: Row) => {
  const base = baseByCharacter.value.get(String(row.character_id))
  return !base || base.kind !== row.kind || base.spoiler !== row.spoiler
}

const isAdded = (row: Row) =>
  !baseByCharacter.value.has(String(row.character_id))

const wireBase = (key: string) => {
  const member = memberOf(key)
  const slot = member ? memberVocabSlot(member) : null
  return slot?.base ?? 0
}

const pick = ref<string | null>(null)
watch(pick, (id) => {
  if (!id) {
    return
  }
  pick.value = null
  if (rows.value.some((row) => String(row.character_id) === id)) {
    return
  }
  value.value = [
    ...rows.value,
    {
      character_id: Number(id),
      kind: wireBase('kind'),
      spoiler: wireBase('spoiler')
    }
  ]
})

const removeAdded = (index: number) => {
  value.value = rows.value.filter((_, i) => i !== index)
}

watch(
  rows,
  (list) =>
    refs.resolve(
      'character',
      list.map((row) => row.character_id)
    ),
  { immediate: true }
)
</script>

<template>
  <div>
    <div v-if="rows.length > 8" class="mb-3 max-w-xs">
      <KunInput
        v-model="filter"
        placeholder="按角色名筛选"
        aria-label="筛选角色"
        size="sm"
      />
    </div>
    <p v-if="!rows.length" class="text-default-400 text-sm">
      这部作品还没有登场角色。
    </p>
    <ul v-else class="divide-default-100 divide-y">
      <li
        v-for="{ row, index } in visibleRows"
        :key="row.character_id"
        :class="
          cn(
            'grid items-center gap-2 py-2 sm:grid-cols-[minmax(0,1fr)_9rem_9rem_auto]',
            isHidden(row) && 'opacity-50',
            changed(row) && 'bg-primary-50 -mx-2 rounded-lg px-2'
          )
        "
      >
        <NuxtLink
          :to="`/characters/${row.character_id}`"
          class="hover:text-primary min-w-0 truncate text-sm"
        >
          {{
            refs.nameOf('character', row.character_id) ||
            `角色 #${row.character_id}`
          }}
          <span class="text-default-400 font-mono text-xs"
            >#{{ row.character_id }}</span
          >
        </NuxtLink>
        <EditMemberInput
          v-if="memberOf('kind')"
          :member="memberOf('kind')!"
          :config="{ label: '出场程度' }"
          :model-value="row.kind"
          :disabled="disabled"
          @update:model-value="setMember(index, 'kind', $event)"
        />
        <EditMemberInput
          v-if="memberOf('spoiler')"
          :member="memberOf('spoiler')!"
          :config="{ label: '剧透等级' }"
          :model-value="row.spoiler"
          :disabled="disabled"
          @update:model-value="setMember(index, 'spoiler', $event)"
        />
        <KunButton
          v-if="isAdded(row) && !disabled"
          size="xs"
          variant="light"
          color="danger"
          @click="removeAdded(index)"
        >
          移除
        </KunButton>
        <KunButton
          v-else-if="companion && identityOf(row) && !disabled"
          size="xs"
          variant="light"
          :color="isHidden(row) ? 'primary' : 'danger'"
          @click="toggleSuppressed(identityOf(row)!)"
        >
          {{ isHidden(row) ? '恢复' : '隐藏' }}
        </KunButton>
        <span v-else />
      </li>
    </ul>
    <div v-if="!disabled" class="mt-3 flex max-w-sm items-center gap-2">
      <EditRefPicker
        v-model="pick"
        family="character"
        :exclude="rows.map((row) => String(row.character_id))"
        aria-label="添加登场角色"
        placeholder="搜索角色并添加到这部作品"
      />
    </div>
  </div>
</template>
