<script setup lang="ts">
import {
  CANDIDATE_REASON_LABELS,
  CANDIDATE_STATUS,
  CANDIDATE_STATUS_COLORS,
  CANDIDATE_STATUS_LABELS,
  CATALOG_ENTITY_TYPE,
  CATALOG_ENTITY_TYPES,
  CATALOG_MEDIUM_LABELS,
  WORK_STATUS
} from '~/constants/catalog'
import type {
  CatalogCandidateItem,
  CatalogEntityRef,
  CatalogEntitySummary,
  CatalogMergeDirection
} from '~~/shared/types/catalog'

const props = defineProps<{ item: CatalogCandidateItem; busy: boolean }>()

const emit = defineEmits<{
  merge: [direction: CatalogMergeDirection]
  reject: []
  defer: []
  release: [workId: number]
}>()

const refKey = (r: CatalogEntityRef) => `${r.source_id}:${r.external_id}`

const sharedKeys = computed(() => {
  const left = new Set((props.item.a.refs ?? []).map(refKey))
  return (props.item.b.refs ?? []).map(refKey).filter((k) => left.has(k))
})

const refCount = (s: CatalogEntitySummary) => s.refs?.length ?? 0

const survivor = computed<'a' | 'b'>(() => {
  const { a, b } = props.item
  if (a.site && !b.site) return 'a'
  if (b.site && !a.site) return 'b'
  if (refCount(a) !== refCount(b)) return refCount(a) > refCount(b) ? 'a' : 'b'
  return a.id <= b.id ? 'a' : 'b'
})

const recommended = computed<CatalogMergeDirection>(() =>
  survivor.value === 'a' ? 'ba' : 'ab'
)

const nameOf = (s: CatalogEntitySummary) => s.display_name || `#${s.id}`

const recommendReason = computed(() => {
  const { a, b } = props.item
  const keep = survivor.value === 'a' ? a : b
  const tag = survivor.value.toUpperCase()
  if (keep.site) return `${tag}「${nameOf(keep)}」已被 ${keep.site} 认领`
  if (refCount(a) !== refCount(b)) {
    return `${tag}「${nameOf(keep)}」外部锚点更多（${refCount(a)} vs ${refCount(b)}）`
  }
  return `${tag}「${nameOf(keep)}」id 更小，铸造更早`
})

const direction = ref<CatalogMergeDirection>(recommended.value)
watch(recommended, (value) => {
  direction.value = value
})

const keepSide = computed(() => (direction.value === 'ab' ? 'b' : 'a'))

const isWork = computed(
  () => props.item.entity_type === CATALOG_ENTITY_TYPE.work
)

const entityNoun = computed(
  () => CATALOG_ENTITY_TYPES[props.item.entity_type] ?? '实体'
)

const isDecidable = computed(() =>
  (
    [
      CANDIDATE_STATUS.pending,
      CANDIDATE_STATUS.deferred,
      CANDIDATE_STATUS.needsManual
    ] as number[]
  ).includes(props.item.status)
)

const nameMismatch = computed(
  () => props.item.a.display_name !== props.item.b.display_name
)

const mediumMismatch = computed(() => {
  const { a, b } = props.item
  return (
    a.medium_id != null && b.medium_id != null && a.medium_id !== b.medium_id
  )
})

const mediumPair = computed(() => {
  const { a, b } = props.item
  const label = (id?: number) =>
    id == null ? '未知' : (CATALOG_MEDIUM_LABELS[id] ?? `媒介${id}`)
  return `${label(a.medium_id)} vs ${label(b.medium_id)}`
})

const bothClaimed = computed(() => !!props.item.a.site && !!props.item.b.site)

const quarantinedIds = computed(() =>
  [props.item.a, props.item.b]
    .filter((s) => s.work_status === WORK_STATUS.QUARANTINE)
    .map((s) => s.id)
)

const sharedRefs = computed(() =>
  (props.item.a.refs ?? []).filter((r) => sharedKeys.value.includes(refKey(r)))
)
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-center gap-2">
      <KunChip color="info" variant="flat" size="xs">
        {{ entityNoun }}
      </KunChip>
      <KunChip color="default" variant="flat" size="xs">
        {{ CANDIDATE_REASON_LABELS[item.reason] ?? item.reason }}
      </KunChip>
      <KunChip
        v-if="item.score !== null"
        color="default"
        variant="flat"
        size="xs"
      >
        相似度 {{ item.score?.toFixed(2) }}
      </KunChip>
      <span class="text-default-400 text-xs">
        {{ formatDate(item.created_at, { isShowYear: true }) }} 入队
      </span>
      <KunChip
        :color="CANDIDATE_STATUS_COLORS[item.status] ?? 'default'"
        variant="flat"
        size="xs"
        class="ml-auto"
      >
        {{ CANDIDATE_STATUS_LABELS[item.status] ?? item.status }}
      </KunChip>
    </div>

    <div
      v-if="sharedKeys.length"
      class="border-success-200 bg-success-50 flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2"
    >
      <KunIcon name="lucide:anchor" class="text-success-600 size-4" />
      <span class="text-success-700 text-sm font-medium">
        {{ sharedKeys.length }} 个共同外部锚点，几乎可以断定是同一{{
          entityNoun
        }}
      </span>
      <CatalogRefChip
        v-for="r in sharedRefs"
        :key="refKey(r)"
        :source-id="r.source_id"
        :external-id="r.external_id"
        :link-kind="r.link_kind"
        :entity-type="item.entity_type"
        shared
      />
    </div>

    <div
      v-if="mediumMismatch"
      class="border-danger-200 bg-danger-50 flex items-center gap-2 rounded-lg border px-3 py-2"
    >
      <KunIcon
        name="lucide:triangle-alert"
        class="text-danger size-4 shrink-0"
      />
      <span class="text-danger-700 text-sm">
        两侧媒介不同（{{
          mediumPair
        }}）。跨媒介同名件是有意分别铸造的，通常不该合并。
      </span>
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <CatalogCandidateSide
        tag="A"
        :entity-type="item.entity_type"
        :summary="item.a"
        :shared-keys="sharedKeys"
        :role="keepSide === 'a' ? 'keep' : 'absorb'"
        :name-mismatch="nameMismatch"
      />
      <CatalogCandidateSide
        tag="B"
        :entity-type="item.entity_type"
        :summary="item.b"
        :shared-keys="sharedKeys"
        :role="keepSide === 'b' ? 'keep' : 'absorb'"
        :name-mismatch="nameMismatch"
      />
    </div>

    <template v-if="isDecidable">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-default-500 text-sm">保留哪一侧：</span>
        <KunButton
          :color="direction === 'ba' ? 'primary' : 'default'"
          :variant="direction === 'ba' ? 'solid' : 'flat'"
          size="sm"
          @click="direction = 'ba'"
        >
          保留 A（B → A）
        </KunButton>
        <KunButton
          :color="direction === 'ab' ? 'primary' : 'default'"
          :variant="direction === 'ab' ? 'solid' : 'flat'"
          size="sm"
          @click="direction = 'ab'"
        >
          保留 B（A → B）
        </KunButton>
        <span class="text-default-400 text-xs">
          推荐保留：{{ recommendReason }}
        </span>
      </div>

      <div
        v-if="bothClaimed"
        class="border-danger-200 bg-danger-50 flex items-center gap-2 rounded-lg border px-3 py-2"
      >
        <KunIcon
          name="lucide:shield-alert"
          class="text-danger size-4 shrink-0"
        />
        <span class="text-danger-700 text-sm">
          两侧都已被站点认领（{{ item.a.site }} /
          {{ item.b.site }}）。合并会释放被并入一侧的认领关系。
        </span>
      </div>

      <div class="flex flex-wrap gap-2">
        <KunButton
          color="success"
          :disabled="busy"
          @click="emit('merge', direction)"
        >
          <KunIcon name="lucide:git-merge" class="mr-1 size-4" />
          合并（保留 {{ keepSide === 'a' ? 'A' : 'B' }}）
        </KunButton>
        <KunButton
          color="danger"
          variant="flat"
          :disabled="busy"
          @click="emit('reject')"
        >
          不是同一{{ entityNoun
          }}{{ isWork && quarantinedIds.length ? '（并放行隔离件）' : '' }}
        </KunButton>
        <KunButton
          color="default"
          variant="flat"
          :disabled="busy"
          @click="emit('defer')"
        >
          搁置
        </KunButton>
      </div>
    </template>

    <div v-else class="flex flex-wrap items-center gap-2">
      <p class="text-default-400 text-sm">
        该候选已决策（{{ CANDIDATE_STATUS_LABELS[item.status] }}
        <template v-if="item.decided_by !== null">
          · 操作者 {{ item.decided_by }}</template
        >）
      </p>
      <KunButton
        v-for="id in quarantinedIds"
        :key="id"
        color="warning"
        variant="flat"
        size="sm"
        :disabled="busy"
        @click="emit('release', id)"
      >
        放行 #{{ id }}
      </KunButton>
    </div>
  </KunCard>
</template>
