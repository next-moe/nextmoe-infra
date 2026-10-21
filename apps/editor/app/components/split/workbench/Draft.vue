<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSplitTitle } from '#shared/types/catalog'
import {
  SPLIT_SOURCE_LABELS,
  SPLIT_SOURCE_URLS,
  SPLIT_TITLE_KIND_OPTIONS
} from '~/constants/split'
import { EDIT_LANGUAGE_LABELS } from '~/constants/vocabLabels'
import { releaseDateText } from '~/utils/merge/plan'
import { refKey } from '~/utils/split/plan'

const workbench = useSplitWorkbench()
const { preview, selection, draft, draftTouched, canEdit, problems } = workbench

const LANG_OPTIONS = Object.entries(EDIT_LANGUAGE_LABELS).map(
  ([value, label]) => ({ value, label })
)
const TITLE_LANG_OPTIONS = [{ value: '', label: '无语言' }, ...LANG_OPTIONS]

const chosen = computed(() =>
  (preview.value?.releases ?? []).filter((release) =>
    selection.value.releases.includes(release.id)
  )
)
const candidates = computed(() => preview.value?.candidates ?? [])
const promoted = (key: string) => selection.value.promote.includes(key)

const dropActive = ref(false)
const onDragOver = (event: DragEvent) => {
  if (
    canEdit.value &&
    event.dataTransfer?.types.includes('application/x-split-add')
  ) {
    event.preventDefault()
    dropActive.value = true
  }
}
const onDrop = (event: DragEvent) => {
  dropActive.value = false
  const ids = event.dataTransfer?.getData('application/x-split-add')
  if (ids) {
    workbench.setReleases([...selection.value.releases, ...ids.split(',')])
  }
}
const onChipDrag = (event: DragEvent, id: string) => {
  if (canEdit.value && event.dataTransfer) {
    event.dataTransfer.setData('application/x-split-remove', id)
    event.dataTransfer.effectAllowed = 'move'
  }
}
const removeRelease = (id: string) =>
  workbench.setReleases(selection.value.releases.filter((r) => r !== id))

const setTitle = (index: number, next: Partial<CatalogSplitTitle>) => {
  const titles = draft.value.titles.map((t, i) =>
    i === index ? { ...t, ...next } : t
  )
  workbench.editDraft({ titles })
}
const addTitle = () =>
  workbench.editDraft({
    titles: [
      ...draft.value.titles,
      { lang: draft.value.olang, title: '', title_kind: 'alias' }
    ]
  })
const removeTitle = (index: number) =>
  workbench.editDraft({
    titles: draft.value.titles.filter((_, i) => i !== index)
  })

const kindLabel = (kind: string) =>
  kind === 'probable' ? '待确认链接' : '相关链接'
</script>

<template>
  <section
    class="border-default-200 min-h-0 flex-col border-r"
    aria-label="新作品"
  >
    <header
      class="border-default-200 flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3"
    >
      <h2 class="flex items-center gap-2 text-sm font-semibold">
        <span class="bg-warning-500 size-2 rounded-full" />
        新作品
      </h2>
      <KunButton
        v-if="canEdit && draftTouched && preview?.suggested_name"
        variant="light"
        size="sm"
        @click="workbench.resetDraftToSuggestion"
      >
        <KunIcon name="lucide:wand-sparkles" class="size-4" />
        按所选发行重新生成
      </KunButton>
    </header>

    <div class="min-h-0 flex-1 space-y-6 overflow-y-auto px-4 py-4">
      <section class="space-y-2" aria-label="移到新作品的发行">
        <h3 class="text-default-600 text-xs font-medium">
          移到新作品的发行 · {{ chosen.length }}
        </h3>
        <div
          role="group"
          aria-label="拖到这里移到新作品"
          :class="
            cn(
              'rounded-xl border border-dashed p-2 transition-colors',
              dropActive
                ? 'border-primary-400 bg-primary-50'
                : 'border-default-300',
              !chosen.length && 'py-6'
            )
          "
          @dragover="onDragOver"
          @dragleave="dropActive = false"
          @drop.prevent="onDrop"
        >
          <p v-if="!chosen.length" class="text-default-400 text-center text-sm">
            在左侧勾选发行，或把发行拖到这里
          </p>
          <ul v-else class="flex flex-wrap gap-1.5">
            <li
              v-for="release in chosen"
              :key="release.id"
              :draggable="canEdit"
              class="border-primary-200 bg-content1 flex max-w-full items-center gap-1.5 rounded-lg border py-1 pr-1 pl-2 text-sm"
              @dragstart="onChipDrag($event, release.id)"
            >
              <span class="text-default-500 shrink-0 text-xs tabular-nums">
                {{ releaseDateText(release) }}
              </span>
              <span class="truncate">{{ release.title || '（无标题）' }}</span>
              <KunButton
                v-if="canEdit"
                variant="light"
                size="xs"
                is-icon-only
                :aria-label="`不拆 ${release.title}`"
                @click="removeRelease(release.id)"
              >
                <KunIcon name="lucide:x" class="size-3.5" />
              </KunButton>
            </li>
          </ul>
        </div>
      </section>

      <section class="space-y-3" aria-label="基本信息">
        <div class="space-y-1.5">
          <span class="text-default-600 text-xs font-medium">显示名</span>
          <KunInput
            aria-label="显示名"
            :model-value="draft.displayName"
            :disabled="!canEdit"
            placeholder="新作品在各站显示的名字"
            @update:model-value="
              (value) =>
                workbench.editDraft({ displayName: String(value ?? '') })
            "
          />
        </div>
        <div class="space-y-1.5">
          <span class="text-default-600 text-xs font-medium">原语言</span>
          <KunSelect
            :model-value="draft.olang"
            :options="LANG_OPTIONS"
            :disabled="!canEdit"
            aria-label="原语言"
            class="max-w-xs"
            @update:model-value="
              (value) => workbench.editDraft({ olang: String(value ?? '') })
            "
          />
        </div>
      </section>

      <section class="space-y-2" aria-label="标题">
        <div class="flex items-center justify-between">
          <h3 class="text-default-600 text-xs font-medium">
            标题 · {{ draft.titles.length }}
          </h3>
          <KunButton v-if="canEdit" variant="light" size="xs" @click="addTitle">
            <KunIcon name="lucide:plus" class="size-3.5" />
            添加标题
          </KunButton>
        </div>
        <p v-if="!draft.titles.length" class="text-default-400 text-sm">
          还没有标题；选好发行后会按发行标题自动生成。
        </p>
        <ul class="space-y-2">
          <li
            v-for="(title, index) in draft.titles"
            :key="index"
            class="grid grid-cols-[7rem_minmax(0,1fr)_6rem_auto] items-center gap-2"
          >
            <KunSelect
              :model-value="title.lang"
              :options="TITLE_LANG_OPTIONS"
              :disabled="!canEdit"
              aria-label="标题语言"
              size="sm"
              @update:model-value="
                (value) => setTitle(index, { lang: String(value ?? '') })
              "
            />
            <KunInput
              :model-value="title.title"
              :disabled="!canEdit"
              size="sm"
              aria-label="标题"
              @update:model-value="
                (value) => setTitle(index, { title: String(value ?? '') })
              "
            />
            <KunSelect
              :model-value="title.title_kind"
              :options="[...SPLIT_TITLE_KIND_OPTIONS]"
              :disabled="!canEdit"
              aria-label="标题类型"
              size="sm"
              @update:model-value="
                (value) =>
                  setTitle(index, {
                    title_kind: value as CatalogSplitTitle['title_kind']
                  })
              "
            />
            <KunButton
              variant="light"
              size="sm"
              is-icon-only
              aria-label="删除这个标题"
              :disabled="!canEdit"
              @click="removeTitle(index)"
            >
              <KunIcon name="lucide:trash-2" class="size-4" />
            </KunButton>
          </li>
        </ul>
      </section>

      <section class="space-y-2" aria-label="资料源对应">
        <h3 class="text-default-600 text-xs font-medium">资料源对应</h3>
        <p class="text-default-500 text-xs leading-relaxed">
          Bangumi、批评空间会把一些版本单独收录。设为新作品的正式对应后，下次导入会从这个条目补齐标题、标签、制作人员；不设则这些资料要人工补。
        </p>
        <ul v-if="candidates.length" class="space-y-2">
          <li
            v-for="ref in candidates"
            :key="refKey(ref)"
            :class="
              cn(
                'rounded-xl border px-3 py-2.5',
                promoted(refKey(ref))
                  ? 'border-primary-200 bg-primary-50'
                  : 'border-default-200'
              )
            "
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 space-y-0.5">
                <a
                  :href="SPLIT_SOURCE_URLS[ref.source]?.(ref.external_id)"
                  target="_blank"
                  rel="noopener"
                  class="hover:text-primary text-sm font-medium"
                >
                  {{ SPLIT_SOURCE_LABELS[ref.source] ?? ref.source }}
                  {{ ref.external_id }}
                  <KunIcon
                    name="lucide:external-link"
                    class="text-default-400 inline size-3.5"
                  />
                </a>
                <p class="text-default-500 text-xs">
                  <template v-if="ref.held_by_id">
                    已经是作品 #{{ ref.held_by_id }}
                    的正式对应，不能再设给新作品
                  </template>
                  <template v-else-if="promoted(refKey(ref))">
                    拆分后成为新作品的正式对应，原作品上不再挂着它
                  </template>
                  <template v-else>
                    目前以{{ kindLabel(ref.anchor_kind) }}挂在原作品上
                  </template>
                </p>
              </div>
              <KunCheckBox
                :model-value="promoted(refKey(ref))"
                :disabled="!canEdit || !!ref.held_by_id"
                label="设为正式对应"
                size="sm"
                color="primary"
                class-name="shrink-0"
                @update:model-value="
                  (value: boolean) =>
                    workbench.togglePromote(refKey(ref), value)
                "
              />
            </div>
          </li>
        </ul>
        <p v-else class="text-default-400 text-sm">
          原作品上没有以相关链接挂着的 Bangumi 或批评空间条目。
        </p>
      </section>

      <KunInfo
        v-if="canEdit && problems.length"
        color="default"
        variant="flat"
        icon="lucide:list-todo"
        title="提交前还需要"
        :description="problems.join('；')"
      />
    </div>
  </section>
</template>
