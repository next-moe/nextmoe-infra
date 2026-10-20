<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogEditImage } from '#shared/types/edit'

const MAX_UPLOAD_BYTES = 20 * 1024 * 1024

const props = defineProps<{ fieldKey: string }>()

const { context, value, baseValue, disabled, config, field } = useEditorField(
  () => props.fieldKey
)
const api = useEditorApi()

const hash = computed(() =>
  typeof value.value === 'string' && value.value ? value.value : ''
)
const image = computed(() =>
  hash.value
    ? (context.images.value[hash.value] ?? {
        url: imageUrl(hash.value),
        thumbhash: null
      })
    : null
)
const changed = computed(() => hash.value !== (baseValue.value ?? ''))

const uploading = ref(false)
const uploadError = ref('')

const upload = async (files: File[]) => {
  const file = files[0]
  if (!file) {
    return
  }
  uploadError.value = ''
  uploading.value = true
  try {
    const form = new FormData()
    form.append('preset', config.value?.imagePreset ?? 'photo')
    form.append('file', file)
    const uploaded = await api<CatalogEditImage>('/v2/me/edit-images', {
      method: 'POST',
      body: form
    })
    context.images.value = {
      ...context.images.value,
      [uploaded.hash]: {
        url: uploaded.url,
        thumbhash: uploaded.thumbhash ?? null
      }
    }
    value.value = uploaded.hash
  } catch (error) {
    uploadError.value = editProblemMessage(parseEditProblem(error))
  } finally {
    uploading.value = false
  }
}

const clear = () => {
  value.value = field.value?.nullable ? null : ''
}
</script>

<template>
  <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
    <div
      :class="
        cn(
          'bg-default-100 border-default-200 w-40 shrink-0 overflow-hidden rounded-lg border',
          changed && 'border-success-300'
        )
      "
    >
      <KunImage
        v-if="image"
        :src="image.url"
        :thumbhash="image.thumbhash ?? undefined"
        :alt="config?.label ?? ''"
        aspect-ratio="1 / 1"
        object-fit="contain"
      />
      <div
        v-else
        class="text-default-400 flex aspect-square flex-col items-center justify-center gap-1 text-xs"
      >
        <KunIcon name="lucide:image-off" class="size-6" />
        暂无图片
      </div>
    </div>
    <div v-if="!disabled" class="space-y-2">
      <KunFileInput
        accept="image/png,image/jpeg,image/webp,image/avif,image/gif"
        :max-size="MAX_UPLOAD_BYTES"
        :disabled="uploading"
        :trigger-text="hash ? '更换图片' : '上传图片'"
        trigger-variant="flat"
        :error="uploadError"
        @change="upload"
      />
      <div class="flex gap-2">
        <KunButton
          v-if="hash"
          size="sm"
          variant="light"
          color="danger"
          @click="clear"
        >
          移除图片
        </KunButton>
        <span
          v-if="uploading"
          class="text-default-500 inline-flex items-center gap-1 text-xs"
        >
          <KunLoading spinner />
          正在上传…
        </span>
      </div>
      <p v-if="hash" class="text-default-400 font-mono text-xs break-all">
        {{ hash }}
      </p>
    </div>
  </div>
</template>
