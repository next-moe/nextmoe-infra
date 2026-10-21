<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogObjectSchema,
  CatalogRevision,
  CatalogRevisionList
} from '#shared/types/catalog'
import {
  ENTITY_FAMILY,
  ORIGIN_SITE_LABELS,
  REVISION_ACTION
} from '~/constants/catalog'
import { useDiffContext } from '~/utils/review/context'
import { useRevisionDiffs } from '~/utils/review/revisionDiff'

useHead({ title: '最近修订 · 未萌编辑台' })

const PAGE_SIZE = 30

const api = useEditorApi()
const names = useEntityNames()
const diffs = useRevisionDiffs()
const { session } = useEditorSession()
const { prepare, context } = useDiffContext()

const family = ref('')
const origin = ref('')
const action = ref('')
const mine = ref(false)
const revisions = ref<CatalogRevision[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const expanded = ref<string | null>(null)
const schemas = ref<Record<string, CatalogObjectSchema>>({})

const familyOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部类型' },
  ...Object.entries(ENTITY_FAMILY).map(([value, { label }]) => ({
    value,
    label
  }))
]

const originOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部来源' },
  ...Object.entries(ORIGIN_SITE_LABELS).map(([value, label]) => ({
    value,
    label
  }))
]

const actionOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部操作' },
  ...Object.entries(REVISION_ACTION).map(([value, { label }]) => ({
    value,
    label
  }))
]

const visible = computed(() =>
  action.value
    ? revisions.value.filter((revision) => revision.action === action.value)
    : revisions.value
)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogRevisionList>('/v2/catalog/revisions', {
      query: {
        limit: PAGE_SIZE,
        ...(family.value ? { object: family.value } : {}),
        ...(origin.value ? { origin_site: origin.value } : {}),
        ...(mine.value && session.value ? { actor_uid: session.value.id } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    revisions.value = cursor ? [...revisions.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
    await names.resolve(list.items)
  } catch {
    if (!cursor) {
      revisions.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

const schemaFor = async (target: string) => {
  if (!schemas.value[target]) {
    const loaded = await api<CatalogObjectSchema>(
      `/v2/catalog/schemas/${target}`
    ).catch(() => null)
    if (loaded) {
      schemas.value = { ...schemas.value, [target]: loaded }
    }
  }
  return schemas.value[target]
}

const toggleDiff = async (revision: CatalogRevision) => {
  if (expanded.value === revision.id) {
    expanded.value = null
    return
  }
  expanded.value = revision.id
  const [schema, entries] = await Promise.all([
    schemaFor(revision.target_object),
    diffs.load(revision.id)
  ])
  if (schema && entries) {
    await prepare(
      schema.fields,
      entries.flatMap((entry) => [
        { key: entry.key, value: entry.from },
        { key: entry.key, value: entry.to }
      ])
    )
  }
}

watch([family, origin, mine], () => fetchPage(null))

fetchPage(null)
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <div class="flex flex-col gap-3">
      <KunHeader
        name="最近修订"
        description="资料库里每一次生效的修改，按时间倒序。"
      />
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <KunSelect
          v-model="family"
          :options="familyOptions"
          aria-label="条目类型"
        />
        <KunSelect
          v-model="origin"
          :options="originOptions"
          aria-label="来源站点"
        />
        <KunSelect
          v-model="action"
          :options="actionOptions"
          aria-label="操作类型"
        />
        <KunSwitch
          v-model="mine"
          label="只看我的修订"
          class-name="self-center"
        />
      </div>
    </div>

    <KunCard bordered padding="md">
      <CommonSkeletonRows v-if="loading && !revisions.length" :rows="8" />
      <ul v-else-if="visible.length" class="divide-default-200 divide-y">
        <HistoryRevisionRow
          v-for="revision in visible"
          :key="revision.id"
          :revision="revision"
          :name="names.nameOf(revision)"
          show-entity
          :expanded="expanded === revision.id"
          @toggle-diff="toggleDiff(revision)"
        >
          <HistoryDiffPanel
            :entries="diffs.stateOf(revision.id)?.entries ?? null"
            :fields="schemas[revision.target_object]?.fields ?? []"
            :context="context"
            before-label="修改前"
            after-label="修改后"
            :loading="diffs.stateOf(revision.id)?.loading"
            :error="diffs.stateOf(revision.id)?.error"
            empty-text="这一版没有字段差异"
          />
        </HistoryRevisionRow>
      </ul>
      <KunNull
        v-else-if="!loading"
        :description="
          revisions.length ? '已加载的修订里没有这种操作' : '暂无修订记录'
        "
      />
      <div class="flex justify-center pt-3">
        <KunLoading v-if="loading && revisions.length" spinner />
        <KunButton
          v-else-if="nextCursor"
          variant="bordered"
          @click="fetchPage(nextCursor)"
        >
          加载更多
        </KunButton>
      </div>
    </KunCard>
  </div>
</template>
