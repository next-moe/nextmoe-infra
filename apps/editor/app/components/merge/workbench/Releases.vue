<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogMergePreviewRelease } from '#shared/types/catalog'
import { EDIT_LANGUAGE_LABELS, vocabLabel } from '~/constants/vocabLabels'
import {
  duplicateReleases,
  mergedReleases,
  releaseDateText
} from '~/utils/merge/plan'

const workbench = useMergeWorkbench()
const { preview, choices, canEditChoices } = workbench
const { byName } = useVocabularies()

const releases = computed(() => preview.value?.releases ?? [])
const bySide = (side: 'source' | 'target') =>
  releases.value.filter((release) => release.side === side)
const result = computed(() =>
  mergedReleases(releases.value, choices.value.hide)
)
const duplicates = computed(() =>
  preview.value ? duplicateReleases(preview.value).map((r) => r.id) : []
)
const allDuplicatesHidden = computed(
  () =>
    duplicates.value.length > 0 &&
    duplicates.value.every((id) => choices.value.hide.includes(id))
)

const highlight = ref<string | null>(null)
const linked = (release: CatalogMergePreviewRelease) =>
  highlight.value !== null &&
  (release.id === highlight.value ||
    release.duplicate_of === highlight.value ||
    releases.value.some(
      (other) =>
        other.id === highlight.value && other.duplicate_of === release.id
    ))

const platformLabel = (token: string) =>
  byName.value.platform?.values.find((v) => v.value === token)?.display_name ||
  token.toUpperCase()
const byId = (id: string | null | undefined) =>
  releases.value.find((release) => release.id === id)
const duplicateText = (release: CatalogMergePreviewRelease) => {
  const other = byId(release.duplicate_of)
  if (!other) {
    return ''
  }
  const how =
    release.duplicate_by === 'vndb'
      ? `同一个 VNDB 发行 ${other.vndb_rid}`
      : '平台、发售日相同'
  return `与保留方「${other.title || releaseDateText(other)}」重复：${how}`
}
const hidden = (id: string) => choices.value.hide.includes(id)
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="releases">
    <template v-if="duplicates.length && canEditChoices" #actions>
      <KunButton
        variant="flat"
        size="sm"
        @click="workbench.setHideAll(duplicates, !allDuplicatesHidden)"
      >
        <KunIcon
          :name="allDuplicatesHidden ? 'lucide:eye' : 'lucide:eye-off'"
          class="size-4"
        />
        {{ allDuplicatesHidden ? '重复的发行都保留显示' : '重复的发行都隐藏' }}
      </KunButton>
    </template>

    <div class="grid gap-3 md:grid-cols-3">
      <MergeWorkbenchCell
        v-for="side in ['source', 'target'] as const"
        :key="side"
        :side="side"
      >
        <ul class="divide-default-100 divide-y px-1 py-1">
          <li
            v-for="release in bySide(side)"
            :key="release.id"
            :class="
              cn(
                'flex items-start gap-2 rounded-lg px-2 py-2 transition-colors',
                linked(release) && 'bg-warning-50'
              )
            "
            @mouseenter="highlight = release.id"
            @mouseleave="highlight = null"
            @focusin="highlight = release.id"
            @focusout="highlight = null"
          >
            <span class="min-w-0 flex-1 space-y-0.5">
              <span class="flex flex-wrap items-center gap-1.5 text-sm">
                <span class="tabular-nums">{{ releaseDateText(release) }}</span>
                <KunChip v-if="release.platform" size="xs" variant="flat">
                  {{ platformLabel(release.platform) }}
                </KunChip>
                <span class="text-default-500 text-xs">
                  {{ vocabLabel('release_kind', release.release_kind) }}
                </span>
              </span>
              <span class="block text-sm break-words">
                {{ release.title || '（无标题）' }}
              </span>
              <span class="text-default-400 flex flex-wrap gap-x-2 text-xs">
                <span v-if="release.lang">
                  {{ EDIT_LANGUAGE_LABELS[release.lang] ?? release.lang }}
                </span>
                <a
                  v-if="release.vndb_rid"
                  :href="`https://vndb.org/${release.vndb_rid}`"
                  target="_blank"
                  rel="noopener"
                  class="hover:text-primary"
                >
                  VNDB {{ release.vndb_rid }}
                </a>
                <span>#{{ release.id }}</span>
              </span>
              <span
                v-if="side === 'source' && release.duplicate_of"
                class="text-warning-700 flex items-start gap-1 text-xs"
              >
                <KunIcon name="lucide:copy" class="mt-0.5 size-3 shrink-0" />
                {{ duplicateText(release) }}
              </span>
            </span>
            <KunCheckBox
              v-if="side === 'source'"
              :model-value="hidden(release.id)"
              :disabled="!canEditChoices"
              color="primary"
              label="合并后隐藏"
              size="sm"
              class-name="shrink-0"
              @update:model-value="
                (value: boolean) => workbench.toggleHide(release.id, value)
              "
            />
          </li>
          <li
            v-if="!bySide(side).length"
            class="text-default-400 px-2 py-3 text-sm"
          >
            没有发行
          </li>
        </ul>
      </MergeWorkbenchCell>

      <MergeWorkbenchCell side="result">
        <ul class="divide-primary-100 divide-y px-1 py-1">
          <li
            v-for="release in result"
            :key="release.id"
            :class="
              cn(
                'flex items-start gap-2 rounded-lg px-2 py-2',
                linked(release) && 'bg-warning-50'
              )
            "
          >
            <span
              :class="
                cn(
                  'mt-1.5 size-1.5 shrink-0 rounded-full',
                  release.side === 'source'
                    ? 'bg-success-500'
                    : 'bg-transparent'
                )
              "
            />
            <span
              :class="
                cn(
                  'min-w-0 flex-1 text-sm',
                  release.hidden && 'text-default-400 line-through'
                )
              "
            >
              <span class="tabular-nums">{{ releaseDateText(release) }}</span>
              · {{ release.title || platformLabel(release.platform || '—') }}
            </span>
            <KunChip v-if="release.hidden" size="xs" variant="flat">
              隐藏
            </KunChip>
          </li>
        </ul>
      </MergeWorkbenchCell>
    </div>
  </MergeWorkbenchSection>
</template>
