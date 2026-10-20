<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { vocabLabel } from '~/constants/vocabLabels'
import type { MergedRow } from '~/utils/mergedRows'

const props = defineProps<{ tagsKey: string; spoilersKey: string }>()

const list = useMergedRows(() => props.tagsKey)
const { context, disabled, rows } = list
const editor = context.editor
const refs = context.refs
const api = useEditorApi()
const { byName } = useVocabularies()

interface TagSummary {
  id: string
  display_name?: string
  tag_kind?: string | null
  is_sexual?: boolean
}

interface TagSource {
  label: string
  count: number | null
}

interface TagView {
  key: string
  id: string
  name: string
  human?: MergedRow
  upstream: MergedRow[]
  sources: TagSource[]
  status: MergedRow['status']
  hideable: MergedRow | null
  count: number
}

const summaries = ref<Record<string, TagSummary>>({})
const loadSummaries = async (ids: string[]) => {
  const missing = ids.filter((id) => id && !summaries.value[id])
  for (let i = 0; i < missing.length; i += 100) {
    const chunk = missing.slice(i, i + 100)
    const found = await api<{ items?: TagSummary[] }>('/v2/catalog/tags', {
      query: { ids: chunk.join(','), nsfw: true }
    }).catch(() => null)
    summaries.value = {
      ...summaries.value,
      ...Object.fromEntries(
        (found?.items ?? []).map((item) => [String(item.id), item])
      )
    }
  }
}

const spoilerField = computed(() =>
  editor.fieldByKey.value.get(props.spoilersKey)
)
const spoilerSlot = computed(() => {
  const member = spoilerField.value?.element?.members?.find(
    (m) => m.key === 'spoiler'
  )
  return member ? memberVocabSlot(member) : null
})
const spoilerDisabled = computed(
  () =>
    disabled.value ||
    !editor.capabilityByKey.value.get(props.spoilersKey)?.can_propose
)

type SpoilerRow = { tag_id: number; spoiler: number }
const spoilerDraft = computed<SpoilerRow[]>(() => {
  const raw = editor.draft.value[props.spoilersKey]
  return Array.isArray(raw) ? (raw as SpoilerRow[]) : []
})
const spoilerBase = computed<SpoilerRow[]>(() => {
  const raw = editor.base.value[props.spoilersKey]
  return Array.isArray(raw) ? (raw as SpoilerRow[]) : []
})
const spoilerIssues = computed(() => [
  ...(editor.issuesByKey.value[props.spoilersKey] ?? []).map(
    (issue) => issue.message
  ),
  ...(editor.serverErrors.value[props.spoilersKey] ?? [])
])
const spoilerOf = (id: string) =>
  spoilerDraft.value.find((row) => String(row.tag_id) === id)?.spoiler
const baseSpoilerOf = (id: string) =>
  spoilerBase.value.find((row) => String(row.tag_id) === id)?.spoiler
const canSetSpoiler = (id: string) => baseSpoilerOf(id) !== undefined
const setSpoiler = (id: string, spoiler: unknown) => {
  editor.setValue(
    props.spoilersKey,
    spoilerDraft.value.map((row) =>
      String(row.tag_id) === id ? { ...row, spoiler: Number(spoiler) } : row
    )
  )
}

const tagIdOf = (row: MergedRow) =>
  row.lane === 'human'
    ? String(row.value ?? '')
    : row.lane === 'upstream'
      ? (row.upstream?.match ?? '')
      : (suppressedKeyParts(row.identity ?? '').refs[0]?.id ?? '')

const STATUS_RANK: Record<MergedRow['status'], number> = {
  removed: 6,
  added: 5,
  changed: 4,
  hiding: 3,
  unhiding: 2,
  hidden: 1,
  kept: 0
}

const views = computed<TagView[]>(() => {
  const byKey = new Map<string, TagView>()
  for (const row of rows.value) {
    const id = tagIdOf(row)
    const raw = (row.value ?? {}) as Record<string, unknown>
    const key = id || `name:${row.upstream?.label ?? row.key}`
    const view = byKey.get(key) ?? {
      key,
      id,
      name: '',
      upstream: [],
      sources: [],
      status: 'kept' as MergedRow['status'],
      hideable: null,
      count: 0
    }
    if (row.lane === 'human') {
      view.human = row
    } else {
      view.upstream.push(row)
      if (row.lane === 'upstream') {
        const count = typeof raw.count === 'number' ? raw.count : null
        view.sources.push({ label: sourceLabelOf(row), count })
        view.count = Math.max(view.count, count ?? 0)
        if (!view.name && typeof raw.name === 'string') {
          view.name = raw.name
        }
      }
      if (!view.hideable && list.canHide(row)) {
        view.hideable = row
      }
    }
    if (STATUS_RANK[row.status] > STATUS_RANK[view.status]) {
      view.status = row.status
    }
    byKey.set(key, view)
  }
  return [...byKey.values()].map((view) => ({
    ...view,
    name:
      (view.id &&
        (summaries.value[view.id]?.display_name ||
          refs.nameOf('tag', view.id))) ||
      view.name ||
      (view.upstream[0]?.lane === 'ghost'
        ? list.ghostLabel(view.upstream[0])
        : view.id
          ? `#${view.id}`
          : '未知标签')
  }))
})

type Category =
  'all' | 'content' | 'sexual' | 'meta' | 'unmapped' | 'human' | 'hidden'
const category = ref<Category>('all')
const query = ref('')

const isHiddenView = (view: TagView) =>
  view.status === 'hidden' || view.status === 'hiding'
const categoryOf = (
  view: TagView
): Exclude<Category, 'all' | 'human' | 'hidden'> => {
  if (!view.id) {
    return 'unmapped'
  }
  const summary = summaries.value[view.id]
  if (summary?.is_sexual) {
    return 'sexual'
  }
  return summary?.tag_kind === 'meta' ? 'meta' : 'content'
}

const SOURCE_ORDER = ['VNDB', 'Bangumi']
const sourceRank = (view: TagView) => {
  const ranks = view.sources.map((source) => {
    const index = SOURCE_ORDER.indexOf(source.label)
    return index < 0 ? SOURCE_ORDER.length : index
  })
  return ranks.length ? Math.min(...ranks) : SOURCE_ORDER.length
}

const counts = computed(() => {
  const out: Record<Category, number> = {
    all: 0,
    content: 0,
    sexual: 0,
    meta: 0,
    unmapped: 0,
    human: 0,
    hidden: 0
  }
  for (const view of views.value) {
    if (isHiddenView(view)) {
      out.hidden++
      continue
    }
    out.all++
    out[categoryOf(view)]++
    if (view.human) {
      out.human++
    }
  }
  return out
})

const CATEGORY_TABS: { value: Category; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'content', label: '内容' },
  { value: 'sexual', label: '色情相关' },
  { value: 'meta', label: '元信息' },
  { value: 'unmapped', label: '未映射' },
  { value: 'human', label: '人工添加' },
  { value: 'hidden', label: '已隐藏' }
]

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  return views.value
    .filter((view) => {
      if (category.value === 'hidden') {
        return isHiddenView(view)
      }
      if (isHiddenView(view) && view.status !== 'hiding') {
        return false
      }
      if (category.value === 'human') {
        return Boolean(view.human)
      }
      return category.value === 'all' || categoryOf(view) === category.value
    })
    .filter(
      (view) =>
        !q ||
        view.name.toLowerCase().includes(q) ||
        view.id === q.replace('#', '')
    )
    .sort(
      (a, b) =>
        Number(Boolean(b.human)) - Number(Boolean(a.human)) ||
        Number(Boolean(b.id)) - Number(Boolean(a.id)) ||
        sourceRank(a) - sourceRank(b) ||
        b.count - a.count ||
        a.name.localeCompare(b.name)
    )
})

const rowOf = (view: TagView): MergedRow =>
  view.human ?? {
    key: view.key,
    lane: view.upstream[0]?.lane ?? 'upstream',
    index: -1,
    value: null,
    identity: view.hideable?.identity ?? view.upstream[0]?.identity ?? null,
    status: view.status
  }

const syncSpoilerEntry = (id: string, present: boolean) => {
  const has = spoilerDraft.value.some((row) => String(row.tag_id) === id)
  if (!present && has) {
    editor.setValue(
      props.spoilersKey,
      spoilerDraft.value.filter((row) => String(row.tag_id) !== id)
    )
  } else if (present && !has) {
    const base = spoilerBase.value.find((row) => String(row.tag_id) === id)
    if (base) {
      editor.setValue(props.spoilersKey, [...spoilerDraft.value, base])
    }
  }
}

const removeHuman = (view: TagView) => {
  if (!view.human) {
    return
  }
  list.removeAt(view.human.index)
  if (!view.upstream.some((row) => row.lane === 'upstream')) {
    syncSpoilerEntry(view.id, false)
  }
}
const hide = (view: TagView) => {
  if (view.hideable) {
    list.hide(view.hideable)
  }
}
const restore = (view: TagView) => {
  const target =
    view.human?.status === 'removed'
      ? view.human
      : view.upstream.find(
          (row) => row.status === 'hidden' || row.status === 'hiding'
        )
  if (target) {
    list.restore(target)
    if (target.lane === 'human') {
      syncSpoilerEntry(view.id, true)
    }
  }
}
const undo = (view: TagView) => {
  const target =
    view.human ?? view.upstream.find((row) => row.status !== 'kept')
  if (target) {
    list.undo(target)
  }
}

const pickerKey = ref(0)
const existingIds = computed(() =>
  views.value.map((view) => view.id).filter(Boolean)
)
const addTag = (id: string | null) => {
  if (!id) {
    return
  }
  const existing = views.value.find((view) => view.id === id)
  if (existing) {
    if (isHiddenView(existing)) {
      restore(existing)
    }
  } else {
    list.append(Number(id))
  }
  pickerKey.value++
}

const spoilerTone = (wire: number | undefined) => {
  const slot = spoilerSlot.value
  const token = slot
    ? vocabTokenOf(slot, byName.value[slot.vocabulary], wire)
    : null
  return token === 'major'
    ? 'text-danger-600'
    : token === 'minor'
      ? 'text-warning-700'
      : 'text-default-600'
}

const spoilerOptions = computed(() => {
  const slot = spoilerSlot.value
  const vocab = slot ? byName.value[slot.vocabulary] : undefined
  return slot && vocab
    ? vocabWireValues(slot, vocab).map(({ token, wire }) => ({
        value: String(wire),
        label: vocabLabel('spoiler', token)
      }))
    : []
})

watch(
  () => views.value.map((view) => view.id).filter(Boolean),
  (ids) => {
    loadSummaries(ids)
  },
  { immediate: true }
)
</script>

<template>
  <div class="@container space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="flex flex-wrap gap-1">
        <button
          v-for="tab in CATEGORY_TABS"
          :key="tab.value"
          type="button"
          :class="
            cn(
              'inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-sm transition-colors',
              category === tab.value
                ? 'bg-primary-50 text-primary-700 font-medium'
                : 'text-default-600 hover:bg-default-100'
            )
          "
          @click="category = tab.value"
        >
          {{ tab.label }}
          <span class="text-default-400 text-xs tabular-nums">
            {{ counts[tab.value] }}
          </span>
        </button>
      </div>
      <span class="flex-1" />
      <KunInput
        v-model="query"
        placeholder="筛选标签"
        aria-label="筛选标签"
        size="sm"
        class="w-full @xl:w-56"
      />
    </div>

    <div
      class="text-default-400 hidden items-center gap-3 px-2 text-xs @3xl:flex"
    >
      <span class="min-w-0 flex-1">标签</span>
      <span class="w-40">来源</span>
      <span class="w-32">剧透等级</span>
      <span class="w-40" />
    </div>

    <ul v-if="filtered.length" class="space-y-0.5">
      <li v-for="view in filtered" :key="view.key">
        <EditListRow
          :row="rowOf(view)"
          :source="view.sources.map((s) => s.label).join(' · ')"
          :can-hide="Boolean(view.hideable)"
          :can-remove="!disabled"
          hide-source
          lock-reason="这个资料源标签还没有映射到规范标签，不能单独隐藏"
          remove-label="移除这个标签"
          trail-class-name="w-auto justify-end @3xl:w-40"
          @remove="removeHuman(view)"
          @hide="hide(view)"
          @restore="restore(view)"
          @undo="undo(view)"
        >
          <div
            class="flex min-w-0 flex-col gap-1 @3xl:flex-row @3xl:items-center @3xl:gap-3"
          >
            <div class="flex min-w-0 flex-1 items-center gap-2">
              <span class="truncate text-sm font-medium">{{ view.name }}</span>
              <KunChip
                v-if="summaries[view.id]?.is_sexual"
                size="xs"
                variant="flat"
                color="secondary"
              >
                色情
              </KunChip>
              <KunChip
                v-else-if="summaries[view.id]?.tag_kind === 'meta'"
                size="xs"
                variant="flat"
              >
                元信息
              </KunChip>
              <KunTooltip
                v-else-if="!view.id"
                text="资料源的原始标签，还没有映射到资料库的规范标签"
                position="top"
              >
                <KunChip size="xs" variant="light">未映射</KunChip>
              </KunTooltip>
            </div>
            <span
              class="text-default-500 flex w-40 shrink-0 flex-wrap items-center gap-1 text-xs"
            >
              <span v-if="view.human" class="text-secondary-600">人工</span>
              <span v-for="source in view.sources" :key="source.label">
                {{ source.label
                }}<span v-if="source.count" class="text-default-400">
                  ×{{ source.count }}</span
                >
              </span>
            </span>
            <div class="w-32 shrink-0">
              <KunTooltip
                v-if="view.id && !canSetSpoiler(view.id)"
                text="新加的标签提交生效后才能设置剧透等级"
                position="top"
              >
                <span class="text-default-400 text-xs">提交后可设置</span>
              </KunTooltip>
              <KunSelect
                v-else-if="view.id && spoilerOptions.length"
                :model-value="String(spoilerOf(view.id) ?? '')"
                :options="spoilerOptions"
                :disabled="spoilerDisabled || isHiddenView(view)"
                aria-label="剧透等级"
                size="sm"
                :class-name="
                  cn(
                    spoilerOf(view.id) !== baseSpoilerOf(view.id) &&
                      'ring-primary-300 rounded-lg ring-2'
                  )
                "
                :class-names="{ trigger: spoilerTone(spoilerOf(view.id)) }"
                @update:model-value="setSpoiler(view.id, $event)"
              />
            </div>
          </div>
        </EditListRow>
      </li>
    </ul>
    <WorkspaceEmpty
      v-else
      icon="lucide:tags"
      :title="query ? '没有匹配的标签' : '这个分类下还没有标签'"
      class-name="py-6"
    />

    <ul v-if="spoilerIssues.length" class="text-danger-600 space-y-1 text-xs">
      <li v-for="(message, i) in spoilerIssues" :key="i">
        剧透等级：{{ message }}
      </li>
    </ul>

    <div v-if="!disabled" class="flex flex-wrap items-center gap-2 pt-1">
      <EditRefPicker
        :key="pickerKey"
        family="tag"
        :exclude="existingIds"
        placeholder="添加标签：搜索名称或输入 ID"
        class="w-full max-w-md"
        @update:model-value="addTag"
      />
      <span class="text-default-400 text-xs">
        资料源的标签无法删除，只能隐藏；隐藏后资料源再次同步也不会加回。
      </span>
    </div>
  </div>
</template>
