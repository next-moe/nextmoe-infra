<script setup lang="ts">
import {
  CANDIDATE_STATUS,
  CANDIDATE_STATUS_COLORS,
  CANDIDATE_STATUS_LABELS,
  CATALOG_ENTITY_TYPE,
  CATALOG_ENTITY_TYPES
} from '~/constants/catalog'
import type { CatalogQueueSummary } from '~~/shared/types/catalog'

const props = defineProps<{
  summary: CatalogQueueSummary | null
  entityType: number
  status: number
}>()

const emit = defineEmits<{ select: [entityType: number, status: number] }>()

const STATUS_ORDER = [
  CANDIDATE_STATUS.needsManual,
  CANDIDATE_STATUS.pending,
  CANDIDATE_STATUS.deferred,
  CANDIDATE_STATUS.accepted,
  CANDIDATE_STATUS.rejected
] as number[]

const buckets = computed(() => props.summary?.candidates ?? [])

const countOf = (entityType: number, status: number) =>
  buckets.value.find((b) => b.entity_type === entityType && b.status === status)
    ?.count ?? 0

const rows = computed(() => {
  const present = [...new Set(buckets.value.map((b) => b.entity_type))]
  const weight = (id: number) =>
    id === CATALOG_ENTITY_TYPE.work
      ? -2
      : id === CATALOG_ENTITY_TYPE.creditName
        ? -1
        : id
  return present
    .sort((a, b) => weight(a) - weight(b))
    .map((id) => ({
      entityType: id,
      label: CATALOG_ENTITY_TYPES[id] ?? `类型${id}`,
      cells: STATUS_ORDER.map((s) => ({ status: s, count: countOf(id, s) })),
      total: buckets.value
        .filter((b) => b.entity_type === id)
        .reduce((sum, b) => sum + b.count, 0)
    }))
})

const workBacklog = computed(() =>
  countOf(CATALOG_ENTITY_TYPE.work, CANDIDATE_STATUS.needsManual)
)

const probableRefTotal = computed(() =>
  (props.summary?.probable_refs ?? []).reduce((sum, b) => sum + b.count, 0)
)

const isActive = (entityType: number, status: number) =>
  props.entityType === entityType && props.status === status
</script>

<template>
  <div class="space-y-3">
    <div
      v-if="workBacklog > 0"
      class="border-warning-200 bg-warning-50 flex flex-wrap items-center gap-4 rounded-xl border p-4"
    >
      <div>
        <p class="text-warning-700 text-sm">作品重复 · 待人工</p>
        <p class="text-warning-800 text-3xl leading-tight font-bold">
          {{ workBacklog }}
        </p>
      </div>
      <p class="text-default-500 min-w-60 flex-1 text-sm">
        机器判不了的作品重复对全部堆在这一格：两侧证据摆在一起，人来裁「合并 /
        不是同一作品 / 搁置」。
      </p>
      <KunButton
        color="warning"
        @click="
          emit('select', CATALOG_ENTITY_TYPE.work, CANDIDATE_STATUS.needsManual)
        "
      >
        <KunIcon name="lucide:list-checks" class="mr-1 size-4" />
        审这一队
      </KunButton>
    </div>

    <KunCard content-class="p-4 space-y-3">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-foreground text-base font-bold">队列全景</h2>
        <span class="text-default-400 text-xs">点任意格子即可筛到那一队</span>
      </div>

      <div class="overflow-x-auto">
        <table class="w-full min-w-[42rem] text-sm">
          <thead class="text-default-500">
            <tr>
              <th class="px-2 py-1 text-left font-medium">实体类型</th>
              <th
                v-for="s in STATUS_ORDER"
                :key="s"
                class="px-2 py-1 text-center font-medium"
              >
                {{ CANDIDATE_STATUS_LABELS[s] }}
              </th>
              <th class="px-2 py-1 text-right font-medium">合计</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in rows"
              :key="row.entityType"
              class="border-default-200 border-t"
            >
              <td class="text-foreground px-2 py-1.5 font-medium">
                {{ row.label }}
              </td>
              <td
                v-for="cell in row.cells"
                :key="cell.status"
                class="px-2 py-1.5 text-center"
              >
                <KunButton
                  v-if="cell.count > 0"
                  :color="
                    isActive(row.entityType, cell.status)
                      ? 'primary'
                      : (CANDIDATE_STATUS_COLORS[cell.status] ?? 'default')
                  "
                  :variant="
                    isActive(row.entityType, cell.status) ? 'solid' : 'flat'
                  "
                  size="xs"
                  @click="emit('select', row.entityType, cell.status)"
                >
                  {{ cell.count }}
                </KunButton>
                <span v-else class="text-default-300">—</span>
              </td>
              <td class="text-default-500 px-2 py-1.5 text-right">
                {{ row.total }}
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td
                :colspan="STATUS_ORDER.length + 2"
                class="text-default-400 px-2 py-6 text-center"
              >
                队列是空的
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div
        v-if="probableRefTotal > 0"
        class="border-default-200 flex flex-wrap items-center gap-2 border-t pt-3"
      >
        <KunIcon name="lucide:link" class="text-default-400 size-4" />
        <span class="text-default-500 text-sm">
          另有 {{ probableRefTotal }} 条待确认的外部链接
        </span>
        <KunButton
          color="default"
          variant="flat"
          size="xs"
          @click="navigateTo('/catalog/refs')"
        >
          去确认
        </KunButton>
      </div>
    </KunCard>
  </div>
</template>
