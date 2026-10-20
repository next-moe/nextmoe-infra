<script setup lang="ts">
import type {
  CatalogEditCapabilities,
  CatalogObjectSchema,
  CatalogRevision,
  CatalogRevisionList
} from '#shared/types/catalog'
import { ENTITY_FAMILY } from '~/constants/catalog'
import { useDiffContext } from '~/utils/review/context'
import { latestRevision, orderPair } from '~/utils/review/history'
import {
  BASELINE_DIFF_BASE,
  useRevisionDiffs
} from '~/utils/review/revisionDiff'

const PAGE_SIZE = 50

const api = useEditorApi()
const route = useRoute()
const names = useEntityNames()
const diffs = useRevisionDiffs()
const { prepare, context } = useDiffContext()

const family = computed(() => String(route.params.family))
const entityId = computed(() => String(route.params.id))
const familyInfo = computed(() => ENTITY_FAMILY[family.value])

const revisions = ref<CatalogRevision[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const schema = ref<CatalogObjectSchema | null>(null)
const capabilities = ref<CatalogEditCapabilities | null>(null)
const expanded = ref<string | null>(null)
const selection = ref<string[]>([])
const compare = ref<{ older: CatalogRevision; newer: CatalogRevision } | null>(
  null
)
const revertTarget = ref<CatalogRevision | null>(null)
const revertBaseline = ref(false)
const revertOpen = ref(false)
const baselineOpen = ref(false)

const fields = computed(() => schema.value?.fields ?? [])
const firstRevision = computed(
  () => revisions.value.find((revision) => revision.seq === 1) ?? null
)
const hasBaseline = computed(() => firstRevision.value?.has_baseline === true)
const latest = computed(() => latestRevision(revisions.value))
const canRevert = computed(
  () => capabilities.value?.fields.some((field) => field.can_review) ?? false
)
const entityName = computed(() =>
  names.nameOf({ target_object: family.value, entity_id: entityId.value })
)

const prepareValues = (values: { key: string; value: unknown }[]) =>
  prepare(fields.value, values)

const prepareEntries = (
  entries: { key: string; from: unknown; to: unknown }[] | null
) =>
  entries
    ? prepareValues(
        entries.flatMap((entry) => [
          { key: entry.key, value: entry.from },
          { key: entry.key, value: entry.to }
        ])
      )
    : Promise.resolve()

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogRevisionList>('/v2/catalog/revisions', {
      query: {
        object: family.value,
        entity_id: entityId.value,
        limit: PAGE_SIZE,
        ...(cursor ? { cursor } : {})
      }
    })
    revisions.value = cursor ? [...revisions.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
  } catch {
    if (!cursor) {
      revisions.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

const loadAll = async () => {
  if (!familyInfo.value) {
    return
  }
  expanded.value = null
  selection.value = []
  compare.value = null
  const [schemaRes, caps] = await Promise.all([
    api<CatalogObjectSchema>(`/v2/catalog/schemas/${family.value}`).catch(
      () => null
    ),
    api<CatalogEditCapabilities>(`/v2/me/edit-capabilities/${family.value}`, {
      query: { entity_id: entityId.value }
    }).catch(() => null),
    fetchPage(null),
    names.resolve([{ target_object: family.value, entity_id: entityId.value }])
  ])
  schema.value = schemaRes
  capabilities.value = caps
}

const toggleDiff = async (revision: CatalogRevision) => {
  if (expanded.value === revision.id) {
    expanded.value = null
    return
  }
  expanded.value = revision.id
  await prepareEntries(await diffs.load(revision.id))
}

const toggleSelect = (revision: CatalogRevision) => {
  const set = selection.value.filter((id) => id !== revision.id)
  if (set.length === selection.value.length) {
    set.push(revision.id)
  }
  selection.value = set.slice(-2)
}

const runCompare = async () => {
  const picked = revisions.value.filter((revision) =>
    selection.value.includes(revision.id)
  )
  if (picked.length !== 2) {
    return
  }
  compare.value = orderPair(picked[0]!, picked[1]!)
  await prepareEntries(
    await diffs.load(compare.value.newer.id, compare.value.older.id)
  )
}

const compareState = computed(() =>
  compare.value
    ? diffs.stateOf(compare.value.newer.id, compare.value.older.id)
    : undefined
)

const askRevert = (revision: CatalogRevision) => {
  revertTarget.value = revision
  revertBaseline.value = false
  revertOpen.value = true
}

const askRevertBaseline = () => {
  if (!firstRevision.value) {
    return
  }
  revertTarget.value = firstRevision.value
  revertBaseline.value = true
  revertOpen.value = true
}

const baselineState = computed(() =>
  latest.value ? diffs.stateOf(latest.value.id, BASELINE_DIFF_BASE) : undefined
)

const toggleBaseline = async () => {
  baselineOpen.value = !baselineOpen.value
  if (baselineOpen.value && latest.value) {
    await prepareEntries(await diffs.load(latest.value.id, BASELINE_DIFF_BASE))
  }
}

watch([family, entityId], loadAll)

useHead(() => ({ title: `修订历史 · ${entityName.value} · 未萌编辑台` }))

await loadAll()
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunInfo
      v-if="!familyInfo"
      color="danger"
      variant="flat"
      title="未知的条目类型"
      :description="`没有「${family}」这种条目。`"
    />

    <template v-else>
      <div
        class="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between"
      >
        <div class="min-w-0">
          <p class="text-default-400 text-sm">修订历史</p>
          <h1 class="sr-only">{{ entityName }} 的修订历史</h1>
          <div class="text-foreground truncate text-xl font-semibold">
            <CommonEntityLink
              :family="family"
              :id="entityId"
              :name="entityName"
            />
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-default-400 text-xs"
            >已选 {{ selection.length }} / 2</span
          >
          <KunButton
            size="sm"
            variant="bordered"
            :disabled="selection.length !== 2"
            @click="runCompare"
          >
            <KunIcon name="lucide:git-compare" class="size-4" />
            对比所选版本
          </KunButton>
        </div>
      </div>

      <KunCard v-if="compare" bordered padding="md" content-class="space-y-3">
        <div class="flex items-center gap-2">
          <h2 class="text-foreground font-medium">
            第 {{ compare.older.seq }} 版 → 第 {{ compare.newer.seq }} 版
          </h2>
          <KunButton
            size="sm"
            variant="light"
            class-name="ml-auto"
            @click="compare = null"
          >
            关闭
          </KunButton>
        </div>
        <HistoryDiffPanel
          :entries="compareState?.entries ?? null"
          :fields="fields"
          :context="context"
          :before-label="`第 ${compare.older.seq} 版`"
          :after-label="`第 ${compare.newer.seq} 版`"
          :loading="compareState?.loading"
          :error="compareState?.error"
        />
      </KunCard>

      <KunCard bordered padding="md">
        <ul v-if="revisions.length" class="divide-default-200 divide-y">
          <HistoryRevisionRow
            v-for="revision in revisions"
            :key="revision.id"
            :revision="revision"
            selectable
            :selected="selection.includes(revision.id)"
            :expanded="expanded === revision.id"
            :can-revert="canRevert"
            :is-latest="latest?.id === revision.id"
            @toggle-select="toggleSelect(revision)"
            @toggle-diff="toggleDiff(revision)"
            @revert="askRevert(revision)"
          >
            <p v-if="revision.seq === 1" class="text-default-400 mb-2 text-xs">
              {{
                revision.has_baseline
                  ? '这是第一次编辑；「修改前」一栏是资料源导入时的原始值。'
                  : '这是第一次编辑，发生在系统开始保存原始状态之前，「修改前」一栏显示为空。'
              }}
            </p>
            <HistoryDiffPanel
              :entries="diffs.stateOf(revision.id)?.entries ?? null"
              :fields="fields"
              :context="context"
              before-label="修改前"
              after-label="修改后"
              :loading="diffs.stateOf(revision.id)?.loading"
              :error="diffs.stateOf(revision.id)?.error"
              empty-text="这一版没有字段差异"
            />
          </HistoryRevisionRow>
          <li v-if="hasBaseline && !nextCursor" class="space-y-3 py-3">
            <div class="flex flex-wrap items-center gap-2">
              <KunIcon name="lucide:database" class="text-default-400 size-4" />
              <span class="text-foreground text-sm font-medium"
                >导入原始状态</span
              >
              <span class="text-default-400 text-xs"
                >第一次人工编辑之前，资料源导入时的样子</span
              >
              <div class="ml-auto flex gap-2">
                <KunButton size="xs" variant="light" @click="toggleBaseline">
                  {{ baselineOpen ? '收起' : '与当前对比' }}
                </KunButton>
                <KunButton
                  v-if="canRevert"
                  size="xs"
                  variant="light"
                  color="warning"
                  @click="askRevertBaseline"
                >
                  回滚到原始状态
                </KunButton>
              </div>
            </div>
            <HistoryDiffPanel
              v-if="baselineOpen"
              :entries="baselineState?.entries ?? null"
              :fields="fields"
              :context="context"
              before-label="导入原始状态"
              after-label="当前"
              :loading="baselineState?.loading"
              :error="baselineState?.error"
              empty-text="当前值与导入原始状态一致"
            />
          </li>
        </ul>
        <KunNull v-else-if="!loading" description="这个条目还没有人工修订" />
        <div class="flex justify-center pt-3">
          <KunLoading v-if="loading" spinner />
          <KunButton
            v-else-if="nextCursor"
            variant="bordered"
            @click="fetchPage(nextCursor)"
          >
            加载更多
          </KunButton>
        </div>
      </KunCard>

      <HistoryRevertModal
        v-model="revertOpen"
        :target="revertTarget"
        :latest="latest"
        :fields="fields"
        :context="context"
        :prepare="prepareValues"
        :baseline="revertBaseline"
        @done="loadAll"
      />
    </template>
  </div>
</template>
