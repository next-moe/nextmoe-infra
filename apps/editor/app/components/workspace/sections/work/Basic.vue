<script setup lang="ts">
import type { CatalogEntitySummary, CatalogWork } from '#shared/types/catalog'
import { CONTENT_RATING, MEDIUM_LABELS } from '~/constants/catalog'
import { vocabLabel } from '~/constants/vocabLabels'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import {
  LINK_KIND_FROM_WIRE,
  LINK_SOURCE_LABELS,
  contextLinkUrl
} from '~/utils/externalLinks'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const { editor, entityId, upstreamRows, config } = useEditorContext()

const { data: summary } = useNuxtData<
  (CatalogEntitySummary & Partial<CatalogWork>) | null
>(`workspace-summary-work-${entityId.value}`)

const draftOf = (field: string) =>
  editor.draft.value[`${config.entityType}.${field}`]

const displayName = computed(() => String(draftOf('display_name') ?? ''))
const olang = computed(() => String(draftOf('olang') ?? ''))
const nsfw = computed(() => draftOf('display_nsfw') === true)

const ratingSlot = computed(() => {
  const field = editor.fieldByKey.value.get(
    `${config.entityType}.content_rating`
  )
  return field ? fieldVocabSlot(field) : null
})
const { byName } = useVocabularies()
const ratingToken = computed(() => {
  const slot = ratingSlot.value
  return slot
    ? vocabTokenOf(
        slot,
        byName.value[slot.vocabulary],
        draftOf('content_rating')
      )
    : null
})
const rating = computed(() =>
  ratingToken.value ? CONTENT_RATING[ratingToken.value] : undefined
)

const identities = computed(() =>
  (upstreamRows.value[`${config.entityType}.links`] ?? [])
    .map((row) => {
      const raw = (row.raw ?? {}) as Record<string, unknown>
      const source = row.sourceKey ?? ''
      const id = String(raw.external_id ?? '')
      return {
        key: row.key,
        source,
        id,
        kind: LINK_KIND_FROM_WIRE[Number(raw.link_kind)] ?? 'related',
        url: contextLinkUrl('work', source, id)
      }
    })
    .filter((entry) => entry.kind === 'identity' && entry.url)
)
</script>

<template>
  <WorkspacePage
    :section="props.section"
    description="作品的显示名、原语言与分级。标题、简介等在左侧对应的分区编辑。"
    wide
  >
    <template #actions>
      <EditListSectionRevert
        :fields="props.section.fields"
        :count="nav.changesBySection.value[props.section.key] ?? 0"
      />
    </template>
    <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <WorkspaceFormSection :fields="props.section.fields" />

      <aside class="space-y-4">
        <div
          class="border-default-200 bg-content1 overflow-hidden rounded-xl border"
        >
          <div class="bg-default-100 flex justify-center">
            <KunImage
              v-if="summary?.cover?.url"
              :src="summary.cover.url"
              :thumbhash="summary.cover.thumbhash ?? undefined"
              :alt="displayName"
              aspect-ratio="3 / 4"
              object-fit="cover"
              class-name="w-full"
            />
            <div
              v-else
              class="text-default-300 flex aspect-[3/4] w-full items-center justify-center"
            >
              <KunIcon name="lucide:image-off" class="size-8" />
            </div>
          </div>
          <div class="space-y-2 p-4">
            <p class="text-default-500 text-xs">列表与页面上显示为</p>
            <p class="leading-6 font-semibold break-words">
              {{ displayName || '（未设置显示名）' }}
            </p>
            <div class="flex flex-wrap gap-1.5">
              <KunChip v-if="summary?.medium" size="xs" variant="flat">
                {{ labelOf(MEDIUM_LABELS, summary.medium) }}
              </KunChip>
              <KunChip
                v-if="rating"
                size="xs"
                variant="flat"
                :color="rating.color"
              >
                {{ rating.label }}
              </KunChip>
              <KunChip v-if="nsfw" size="xs" variant="flat" color="danger">
                按 NSFW 显示
              </KunChip>
              <KunChip v-if="olang" size="xs" variant="flat">
                原语言 {{ vocabLabel('olang', olang) }}
              </KunChip>
            </div>
          </div>
        </div>

        <div
          v-if="identities.length"
          class="border-default-200 bg-content1 rounded-xl border p-4"
        >
          <p class="text-default-500 mb-2 text-xs">对应的外部条目</p>
          <ul class="space-y-1.5">
            <li
              v-for="entry in identities"
              :key="entry.key"
              class="flex items-center justify-between gap-2 text-sm"
            >
              <span class="text-default-600">
                {{ LINK_SOURCE_LABELS[entry.source] ?? entry.source }}
              </span>
              <a
                :href="entry.url!"
                target="_blank"
                rel="noopener noreferrer"
                class="text-primary-600 truncate font-mono text-xs hover:underline"
              >
                {{ entry.id }}
              </a>
            </li>
          </ul>
          <button
            type="button"
            class="text-default-500 hover:text-foreground mt-3 text-xs"
            @click="nav.go('links')"
          >
            管理外链 →
          </button>
        </div>
      </aside>
    </div>
  </WorkspacePage>
</template>
