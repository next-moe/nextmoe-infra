<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogEditImage, CatalogSchemaMember } from '#shared/types/edit'

const MAX_UPLOAD_BYTES = 20 * 1024 * 1024
const MEMBER_LABELS: Record<string, string> = {
  kind: '类型',
  portrait_pinned: '竖版主封面',
  sexual: '性暗示',
  violence: '暴力',
  caption: '说明'
}

const props = defineProps<{ fieldKey: string }>()

const { context, value, baseValue, disabled, config, field, issues } =
  useEditorField(() => props.fieldKey)
const api = useEditorApi()

type Row = Record<string, unknown>

const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseHashes = computed(
  () =>
    new Set(
      (Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []).map(
        (row) => String(row.image_hash)
      )
    )
)
const members = computed<CatalogSchemaMember[]>(() =>
  (field.value?.element?.members ?? []).filter((m) => m.key !== 'image_hash')
)
const cap = computed(() => field.value?.max_elements ?? 0)

const update = (next: Row[]) => {
  value.value = next
}
const reorder = useListReorder(() => rows.value, update)

const setMember = (index: number, key: string, next: unknown) => {
  update(
    rows.value.map((row, i) => (i === index ? { ...row, [key]: next } : row))
  )
}

const imageOf = (row: Row) => context.images.value[String(row.image_hash)]

const uploading = ref(0)
const uploadError = ref('')

const upload = async (files: File[]) => {
  uploadError.value = ''
  for (const file of files) {
    if (cap.value && rows.value.length >= cap.value) {
      uploadError.value = `最多 ${cap.value} 张`
      break
    }
    uploading.value++
    try {
      const form = new FormData()
      form.append('preset', config.value?.imagePreset ?? 'cover')
      form.append('file', file)
      const image = await api<CatalogEditImage>('/v2/me/edit-images', {
        method: 'POST',
        body: form
      })
      context.images.value = {
        ...context.images.value,
        [image.hash]: { url: image.url, thumbhash: image.thumbhash ?? null }
      }
      if (!rows.value.some((row) => row.image_hash === image.hash)) {
        update([...rows.value, { image_hash: image.hash }])
      }
    } catch (error) {
      uploadError.value = `${file.name}：${editProblemMessage(parseEditProblem(error))}`
    } finally {
      uploading.value--
    }
  }
}

const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)
</script>

<template>
  <div>
    <div
      v-if="rows.length"
      class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3"
    >
      <article
        v-for="(row, index) in rows"
        :key="String(row.image_hash)"
        v-bind="disabled ? {} : reorder.dropAttrs(index)"
        :class="
          cn(
            'border-default-200 overflow-hidden rounded-lg border',
            !baseHashes.has(String(row.image_hash)) && 'border-success-300',
            reorder.overIndex.value === index && 'ring-primary-300 ring-2'
          )
        "
      >
        <div class="bg-default-100 relative">
          <KunImage
            v-if="imageOf(row)"
            :src="imageOf(row)!.url"
            :thumbhash="imageOf(row)!.thumbhash ?? undefined"
            :alt="String(row.caption ?? row.kind ?? '')"
            aspect-ratio="4 / 3"
            object-fit="contain"
          />
          <div
            v-else
            class="text-default-400 flex aspect-[4/3] flex-col items-center justify-center gap-1 text-xs"
          >
            <KunIcon name="lucide:image" class="size-6" />
            <span class="max-w-full truncate px-2 font-mono">
              {{ String(row.image_hash).slice(0, 12) }}…
            </span>
          </div>
          <span
            class="bg-background/80 text-default-600 absolute top-1.5 left-1.5 rounded px-1.5 text-xs tabular-nums"
          >
            {{ index + 1 }}
          </span>
          <EditDragHandle
            v-if="!disabled"
            v-bind="reorder.handleAttrs(index)"
            class="bg-background/80 absolute top-1 right-1"
            @keydown="reorder.onKeydown($event, index)"
          />
        </div>
        <div class="space-y-2 p-2">
          <div class="grid grid-cols-2 gap-2">
            <div
              v-for="member in members"
              :key="member.key"
              :class="
                cn('min-w-0 space-y-1', member.type === 'text' && 'col-span-2')
              "
            >
              <span class="text-default-500 text-xs">
                {{ MEMBER_LABELS[member.key] ?? member.key }}
              </span>
              <EditMemberInput
                :member="member"
                :config="{ label: MEMBER_LABELS[member.key] ?? member.key }"
                :model-value="row[member.key]"
                :disabled="disabled"
                :invalid="memberInvalid(index, member.key)"
                @update:model-value="setMember(index, member.key, $event)"
              />
            </div>
          </div>
          <div class="flex justify-end">
            <EditRowActions
              :index="index"
              :count="rows.length"
              :disabled="disabled"
              @move="reorder.move"
              @remove="update(rows.filter((_, i) => i !== index))"
            />
          </div>
        </div>
      </article>
    </div>
    <p v-else class="text-default-400 text-sm">还没有人工上传的图片。</p>

    <div v-if="!disabled" class="mt-3 flex flex-wrap items-center gap-3">
      <KunFileInput
        accept="image/png,image/jpeg,image/webp,image/avif,image/gif"
        multiple
        :max-size="MAX_UPLOAD_BYTES"
        :disabled="uploading > 0 || (cap > 0 && rows.length >= cap)"
        trigger-text="上传图片"
        trigger-variant="flat"
        :error="uploadError"
        @change="upload"
      />
      <span
        v-if="uploading"
        class="text-default-500 inline-flex items-center gap-1 text-xs"
      >
        <KunLoading spinner />
        正在上传 {{ uploading }} 张…
      </span>
    </div>

    <EditUpstream
      :field-key="props.fieldKey"
      :exclude="[...baseHashes]"
      images
    />
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
</template>
