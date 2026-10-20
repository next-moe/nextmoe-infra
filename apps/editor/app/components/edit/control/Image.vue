<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = defineProps<{ fieldKey: string }>()

const { context, value, baseValue, disabled, config, field } = useEditorField(
  () => props.fieldKey
)
const api = useEditorApi()

const picker = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const uploadError = ref('')
const hover = ref(0)

const hash = computed(() =>
  typeof value.value === 'string' && value.value ? value.value : ''
)
const baseHash = computed(() =>
  typeof baseValue.value === 'string' && baseValue.value ? baseValue.value : ''
)
const imageOf = (key: string) =>
  key
    ? (context.images.value[key] ?? { url: imageUrl(key), thumbhash: null })
    : null
const image = computed(() => imageOf(hash.value))
const previous = computed(() => imageOf(baseHash.value))
const changed = computed(() => hash.value !== baseHash.value)
const label = computed(() => config.value?.label ?? '图片')

const upload = async (files: File[]) => {
  const file = files[0]
  if (!file || disabled.value) {
    return
  }
  uploadError.value = ''
  if (!isImageFile(file)) {
    uploadError.value = '只支持 PNG、JPEG、WebP、AVIF 和 GIF 图片'
    return
  }
  if (file.size > EDIT_IMAGE_MAX_BYTES) {
    uploadError.value = '图片不能超过 20 MB'
    return
  }
  uploading.value = true
  try {
    const uploaded = await uploadEditImage(
      api,
      config.value?.imagePreset ?? 'photo',
      file
    )
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

const openPicker = () => {
  if (!disabled.value && !uploading.value) {
    picker.value?.click()
  }
}

const onPick = (event: Event) => {
  const input = event.target as HTMLInputElement
  upload(Array.from(input.files ?? []))
  input.value = ''
}

const onDragEnter = (event: DragEvent) => {
  if (hasDraggedFiles(event) && !disabled.value) {
    hover.value++
  }
}
const onDragLeave = (event: DragEvent) => {
  if (hasDraggedFiles(event) && hover.value > 0) {
    hover.value--
  }
}
const onDragOver = (event: DragEvent) => {
  if (hasDraggedFiles(event) && !disabled.value) {
    event.preventDefault()
  }
}
const onDrop = (event: DragEvent) => {
  if (!hasDraggedFiles(event)) {
    return
  }
  event.preventDefault()
  hover.value = 0
  upload(Array.from(event.dataTransfer?.files ?? []))
}

const clear = () => {
  value.value = field.value?.nullable ? null : ''
}
const revert = () => {
  value.value = baseValue.value ?? (field.value?.nullable ? null : '')
}
</script>

<template>
  <div class="flex flex-col gap-4 sm:flex-row sm:items-start">
    <button
      type="button"
      :disabled="disabled"
      :aria-label="hash ? `更换${label}` : `上传${label}`"
      :class="
        cn(
          'group bg-default-100 relative block w-44 shrink-0 overflow-hidden rounded-xl border-2 text-left transition-colors',
          hover > 0
            ? 'border-primary-400 bg-primary-50 border-dashed'
            : image
              ? changed
                ? 'border-success-400'
                : 'border-default-200'
              : 'border-default-300 border-dashed',
          !disabled && 'hover:border-primary-300 cursor-pointer',
          'focus-visible:ring-primary-400 focus-visible:ring-2 focus-visible:outline-none'
        )
      "
      @click="openPicker"
      @dragenter="onDragEnter"
      @dragleave="onDragLeave"
      @dragover="onDragOver"
      @drop="onDrop"
    >
      <KunImage
        v-if="image"
        :src="image.url"
        :thumbhash="image.thumbhash ?? undefined"
        :alt="label"
        aspect-ratio="1 / 1"
        object-fit="contain"
      />
      <div
        v-else
        class="text-default-500 flex aspect-square flex-col items-center justify-center gap-1.5 px-3 text-center text-xs"
      >
        <KunIcon name="lucide:image-up" class="text-default-400 size-7" />
        <span v-if="disabled">暂无{{ label }}</span>
        <template v-else>
          <span class="text-default-700 font-medium">拖入或点击上传</span>
          <span>PNG、JPEG、WebP，最大 20 MB</span>
        </template>
      </div>
      <div
        v-if="image && !disabled && !uploading"
        class="bg-background/80 text-default-700 absolute inset-x-0 bottom-0 flex items-center justify-center gap-1 py-1.5 text-xs font-medium opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
      >
        <KunIcon name="lucide:upload" class="size-3.5" />
        更换{{ label }}
      </div>
      <div
        v-if="uploading"
        class="bg-background/80 text-default-600 absolute inset-0 flex flex-col items-center justify-center gap-2 text-xs"
      >
        <KunLoading spinner />
        正在上传…
      </div>
      <span
        v-if="changed && hash"
        class="bg-success-500 text-success-foreground absolute top-2 left-2 rounded px-1.5 text-[11px] leading-5 font-medium"
      >
        新图片
      </span>
    </button>
    <input
      ref="picker"
      type="file"
      class="hidden"
      :aria-label="`选择${label}文件`"
      :accept="EDIT_IMAGE_ACCEPT"
      @change="onPick"
    />

    <div class="min-w-0 space-y-3">
      <div v-if="!disabled" class="flex flex-wrap gap-2">
        <KunButton
          size="sm"
          variant="flat"
          :disabled="uploading"
          @click="openPicker"
        >
          <KunIcon name="lucide:upload" class="size-4" />
          {{ hash ? `更换${label}` : `上传${label}` }}
        </KunButton>
        <KunButton
          v-if="hash"
          size="sm"
          variant="light"
          color="danger"
          :disabled="uploading"
          @click="clear"
        >
          <KunIcon name="lucide:trash-2" class="size-4" />
          移除
        </KunButton>
        <KunButton
          v-if="changed"
          size="sm"
          variant="light"
          color="default"
          :disabled="uploading"
          @click="revert"
        >
          <KunIcon name="lucide:undo-2" class="size-4" />
          撤销
        </KunButton>
      </div>
      <p
        v-if="uploadError"
        class="text-danger-600 flex items-start gap-1 text-xs"
      >
        <KunIcon name="lucide:circle-alert" class="mt-0.5 size-3.5 shrink-0" />
        {{ uploadError }}
      </p>
      <div v-if="changed && previous" class="flex items-center gap-2">
        <div
          class="border-default-200 bg-default-100 w-12 overflow-hidden rounded-md border"
        >
          <KunImage
            :src="previous.url"
            alt=""
            aspect-ratio="1 / 1"
            object-fit="contain"
            class-name="opacity-70"
          />
        </div>
        <span class="text-default-500 text-xs">修改前的{{ label }}</span>
      </div>
      <p
        v-if="!hash && !changed && !disabled"
        class="text-default-500 text-xs leading-5"
      >
        也可以把图片文件直接拖到左侧方框里。
      </p>
    </div>
  </div>
</template>
