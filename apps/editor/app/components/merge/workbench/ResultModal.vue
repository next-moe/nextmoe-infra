<script setup lang="ts">
import { MERGE_FIELD_LABELS, MERGE_LINK_KIND_LABELS } from '~/constants/merge'
import { EDIT_LANGUAGE_LABELS } from '~/constants/vocabLabels'
import { LINK_SOURCE_LABELS } from '~/utils/externalLinks'
import { diffText } from '#shared/utils/diff'
import {
  chosenValue,
  countAfter,
  mergedTitles,
  releaseDateText
} from '~/utils/merge/plan'

const open = defineModel<boolean>({ required: true })

const { preview, choices } = useMergeWorkbench()

const changedFields = computed(() =>
  (preview.value?.fields ?? [])
    .map((field) => ({
      field,
      before: field.target_value,
      after: chosenValue(field, choices.value)
    }))
    .filter(
      ({ before, after }) => JSON.stringify(before) !== JSON.stringify(after)
    )
)
const newTitles = computed(() =>
  mergedTitles(preview.value?.titles ?? []).filter(
    (title) => title.side === 'source'
  )
)
const releases = computed(() =>
  (preview.value?.releases ?? []).filter((release) => release.side === 'source')
)
const anchors = computed(() =>
  (preview.value?.anchors ?? []).filter(
    (anchor) => anchor.side === 'source' && anchor.fate !== 'duplicate'
  )
)
const counts = computed(() =>
  (preview.value?.counts ?? []).filter(
    (count) => countAfter(count) !== count.on_target
  )
)
const asText = (value: unknown) =>
  value === null || value === undefined ? '' : String(value)
</script>

<template>
  <KunModal
    v-model="open"
    title="合并后的保留方"
    :description="
      preview
        ? `与现在的「${preview.target.name}」相比，合并会带来这些变化。`
        : ''
    "
    size="xl"
  >
    <div v-if="preview" class="max-h-[70vh] space-y-5 overflow-y-auto pr-1">
      <section v-if="changedFields.length" class="space-y-2">
        <h3 class="text-sm font-medium">基本信息</h3>
        <ul class="space-y-1.5">
          <li
            v-for="{ field, before, after } in changedFields"
            :key="field.key"
            class="bg-default-50 rounded-lg px-3 py-2 text-sm"
          >
            <span class="text-default-500 mr-2">
              {{ MERGE_FIELD_LABELS[field.key] ?? field.key }}
            </span>
            <DiffText :segments="diffText(asText(before), asText(after))" />
          </li>
        </ul>
      </section>

      <section v-if="newTitles.length" class="space-y-2">
        <h3 class="text-sm font-medium">新增标题 {{ newTitles.length }}</h3>
        <ul class="flex flex-wrap gap-2">
          <li
            v-for="(title, index) in newTitles"
            :key="index"
            class="bg-success-50 text-success-800 rounded-lg px-2.5 py-1 text-sm"
          >
            {{ title.title }}
            <span class="text-success-600 text-xs">
              {{
                title.lang
                  ? (EDIT_LANGUAGE_LABELS[title.lang] ?? title.lang)
                  : '别名'
              }}
            </span>
          </li>
        </ul>
      </section>

      <section v-if="releases.length" class="space-y-2">
        <h3 class="text-sm font-medium">移入的发行 {{ releases.length }}</h3>
        <ul class="space-y-1 text-sm">
          <li
            v-for="release in releases"
            :key="release.id"
            class="flex items-center gap-2"
          >
            <KunIcon
              :name="
                choices.hide.includes(release.id)
                  ? 'lucide:eye-off'
                  : 'lucide:plus'
              "
              :class="
                choices.hide.includes(release.id)
                  ? 'text-default-400 size-4'
                  : 'text-success-600 size-4'
              "
            />
            <span class="tabular-nums">{{ releaseDateText(release) }}</span>
            <span class="min-w-0 truncate">{{
              release.title || release.platform
            }}</span>
            <span
              v-if="choices.hide.includes(release.id)"
              class="text-default-400 text-xs"
            >
              合并后隐藏
            </span>
          </li>
        </ul>
      </section>

      <section v-if="anchors.length" class="space-y-2">
        <h3 class="text-sm font-medium">资料源对应</h3>
        <ul class="space-y-1 text-sm">
          <li
            v-for="anchor in anchors"
            :key="`${anchor.source_key}:${anchor.external_id}`"
          >
            {{ LINK_SOURCE_LABELS[anchor.source_key] ?? anchor.source_key }}
            {{ anchor.external_id }}：
            <span
              :class="
                anchor.kind_after === anchor.kind_before
                  ? 'text-success-700'
                  : 'text-warning-700'
              "
            >
              {{ MERGE_LINK_KIND_LABELS[anchor.kind_after] }}
            </span>
          </li>
        </ul>
      </section>

      <section v-if="counts.length" class="space-y-2">
        <h3 class="text-sm font-medium">数量变化</h3>
        <MergeWorkbenchCountTable :counts="counts" />
      </section>
    </div>
  </KunModal>
</template>
