<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSplitPreviewRelease } from '#shared/types/catalog'
import { SPLIT_GROUP_OPTIONS } from '~/constants/split'
import { EDIT_LANGUAGE_LABELS, vocabLabel } from '~/constants/vocabLabels'
import { releaseDateText } from '~/utils/merge/plan'
import {
  groupReleases,
  rangeSelect,
  releaseMatches,
  type SplitGroupBy
} from '~/utils/split/plan'

const workbench = useSplitWorkbench()
const { preview, previewLoading, selection, canEdit } = workbench
const { byName } = useVocabularies()

const query = ref('')
const groupBy = ref<SplitGroupBy>('kind')
const anchor = ref<string | null>(null)
const dropActive = ref(false)

const releases = computed(() => preview.value?.releases ?? [])
const visible = computed(() =>
  releases.value.filter((release) => releaseMatches(release, query.value))
)
const groups = computed(() => groupReleases(visible.value, groupBy.value))
const visibleIds = computed(() =>
  groups.value.flatMap((group) => group.releases.map((release) => release.id))
)
const selectedCount = computed(() => selection.value.releases.length)
const allVisibleSelected = computed(
  () =>
    visibleIds.value.length > 0 &&
    visibleIds.value.every((id) => selection.value.releases.includes(id))
)

const platformLabel = (token: string) =>
  byName.value.platform?.values.find((v) => v.value === token)?.display_name ||
  token.toUpperCase()
const groupLabel = (key: string) => {
  if (groupBy.value === 'kind') {
    return vocabLabel('release_kind', key)
  }
  if (groupBy.value === 'platform') {
    return key ? platformLabel(key) : '平台不明'
  }
  return key ? (EDIT_LANGUAGE_LABELS[key] ?? key) : '语言不明'
}

const isSelected = (id: string) => selection.value.releases.includes(id)

const toggle = (release: CatalogSplitPreviewRelease, shift: boolean) => {
  if (!canEdit.value) {
    return
  }
  workbench.setReleases(
    rangeSelect(
      visibleIds.value,
      selection.value.releases,
      anchor.value,
      release.id,
      shift
    )
  )
  anchor.value = release.id
}

const selectVisible = (on: boolean) => {
  const rest = selection.value.releases.filter(
    (id) => !visibleIds.value.includes(id)
  )
  workbench.setReleases(on ? [...rest, ...visibleIds.value] : rest)
}

const onDragStart = (event: DragEvent, release: CatalogSplitPreviewRelease) => {
  if (!canEdit.value || !event.dataTransfer) {
    return
  }
  const ids = isSelected(release.id)
    ? selection.value.releases
    : [...selection.value.releases, release.id]
  event.dataTransfer.setData('application/x-split-add', ids.join(','))
  event.dataTransfer.effectAllowed = 'move'
}

const onDrop = (event: DragEvent) => {
  dropActive.value = false
  const ids = event.dataTransfer?.getData('application/x-split-remove')
  if (ids) {
    const drop = ids.split(',')
    workbench.setReleases(
      selection.value.releases.filter((id) => !drop.includes(id))
    )
  }
}
const onDragOver = (event: DragEvent) => {
  if (event.dataTransfer?.types.includes('application/x-split-remove')) {
    event.preventDefault()
    dropActive.value = true
  }
}
</script>

<template>
  <section
    :class="
      cn(
        'border-default-200 min-h-0 flex-col border-r transition-colors',
        dropActive && 'bg-default-100'
      )
    "
    aria-label="原作品的发行"
    @dragover="onDragOver"
    @dragleave="dropActive = false"
    @drop.prevent="onDrop"
  >
    <header class="border-default-200 shrink-0 space-y-2.5 border-b px-4 py-3">
      <div class="flex items-center justify-between gap-2">
        <h2 class="flex items-center gap-2 text-sm font-semibold">
          <span class="bg-success-500 size-2 rounded-full" />
          原作品的发行
          <span class="text-default-400 font-normal tabular-nums">
            {{ releases.length }}
          </span>
        </h2>
        <span
          :class="
            cn(
              'text-xs tabular-nums',
              selectedCount ? 'text-primary-600' : 'text-default-400'
            )
          "
        >
          已选 {{ selectedCount }} 个拆出去
        </span>
      </div>
      <div class="flex items-center gap-2">
        <KunInput
          v-model="query"
          size="sm"
          placeholder="按标题、平台、语言、VNDB 编号筛选"
          aria-label="筛选发行"
          class-name="min-w-0 flex-1"
        >
          <template #prefix>
            <KunIcon name="lucide:search" class="text-default-400 size-4" />
          </template>
        </KunInput>
        <KunSelect
          v-model="groupBy"
          :options="[...SPLIT_GROUP_OPTIONS]"
          aria-label="分组方式"
          size="sm"
          class="w-32 shrink-0"
        />
      </div>
      <div
        v-if="canEdit && visibleIds.length"
        class="text-default-500 flex items-center gap-3 text-xs"
      >
        <button
          type="button"
          class="hover:text-primary"
          @click="selectVisible(!allVisibleSelected)"
        >
          {{ allVisibleSelected ? '取消选择筛选结果' : '选择筛选结果' }}
        </button>
        <span class="text-default-300">·</span>
        <span>按住 Shift 点击可以连选，也可以把发行拖到中间</span>
      </div>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto px-2 py-2">
      <CommonSkeletonRows
        v-if="previewLoading && !preview"
        :rows="8"
        class="px-2"
      />
      <KunNull
        v-else-if="!visible.length"
        :description="query ? '没有符合筛选的发行' : '这部作品没有发行'"
      />
      <template v-else>
        <div v-for="group in groups" :key="group.key" class="pb-2">
          <p
            v-if="groupBy !== 'none'"
            class="text-default-400 bg-background sticky top-0 z-[1] px-2 py-1.5 text-xs font-medium"
          >
            {{ groupLabel(group.key) }} · {{ group.releases.length }}
          </p>
          <ul class="space-y-1">
            <li v-for="release in group.releases" :key="release.id">
              <div
                role="checkbox"
                :aria-checked="isSelected(release.id)"
                :aria-disabled="!canEdit"
                :tabindex="0"
                :draggable="canEdit"
                :class="
                  cn(
                    'group flex cursor-pointer items-start gap-2.5 rounded-lg border px-2.5 py-2 transition-colors select-none',
                    isSelected(release.id)
                      ? 'border-primary-200 bg-primary-50'
                      : 'hover:bg-default-100 border-transparent',
                    !canEdit && 'cursor-default'
                  )
                "
                @click="toggle(release, $event.shiftKey)"
                @keydown.space.prevent="toggle(release, $event.shiftKey)"
                @keydown.enter.prevent="toggle(release, $event.shiftKey)"
                @dragstart="onDragStart($event, release)"
              >
                <KunIcon
                  :name="
                    isSelected(release.id)
                      ? 'lucide:square-check'
                      : 'lucide:square'
                  "
                  :class="
                    cn(
                      'mt-0.5 size-4 shrink-0',
                      isSelected(release.id)
                        ? 'text-primary-600'
                        : 'text-default-300'
                    )
                  "
                />
                <span class="min-w-0 flex-1 space-y-0.5">
                  <span class="flex flex-wrap items-center gap-1.5 text-sm">
                    <span class="text-default-600 tabular-nums">
                      {{ releaseDateText(release) }}
                    </span>
                    <KunChip v-if="release.platform" size="xs" variant="flat">
                      {{ platformLabel(release.platform) }}
                    </KunChip>
                    <span
                      v-if="groupBy !== 'kind'"
                      class="text-default-500 text-xs"
                    >
                      {{ vocabLabel('release_kind', release.release_kind) }}
                    </span>
                  </span>
                  <span class="block text-sm font-medium break-words">
                    {{ release.title || '（无标题）' }}
                  </span>
                  <span
                    class="text-default-400 flex flex-wrap items-center gap-x-2 text-xs"
                  >
                    <span v-if="release.lang && groupBy !== 'lang'">
                      {{ EDIT_LANGUAGE_LABELS[release.lang] ?? release.lang }}
                    </span>
                    <a
                      v-if="release.vndb_rid"
                      :href="`https://vndb.org/${release.vndb_rid}`"
                      target="_blank"
                      rel="noopener"
                      class="hover:text-primary"
                      @click.stop
                    >
                      VNDB {{ release.vndb_rid }}
                    </a>
                    <span
                      v-for="source in release.sources.filter(
                        (s) => s !== 'vndb'
                      )"
                      :key="source"
                    >
                      {{ source }}
                    </span>
                    <span>#{{ release.id }}</span>
                  </span>
                  <span
                    v-if="
                      isSelected(release.id) &&
                      (release.label_count || release.credit_count)
                    "
                    class="text-primary-600 block text-xs"
                  >
                    一起带走：
                    <template v-if="release.label_count">
                      {{ release.label_count }} 个厂商
                    </template>
                    <template v-if="release.label_count && release.credit_count"
                      >、</template
                    >
                    <template v-if="release.credit_count">
                      {{ release.credit_count }} 条只限这个发行的署名
                    </template>
                  </span>
                </span>
                <WorkspaceHint
                  v-if="isSelected(release.id) && release.tombstone"
                  text="原作品里会留下一条「已移走」记录，VNDB 导入不会把它建回来"
                  position="left"
                >
                  <KunIcon
                    name="lucide:bookmark-x"
                    class="text-default-400 mt-0.5 size-4 shrink-0"
                  />
                </WorkspaceHint>
              </div>
            </li>
          </ul>
        </div>
      </template>
    </div>
  </section>
</template>
