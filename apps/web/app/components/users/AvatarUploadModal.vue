<script setup lang="ts">

interface Props {
  open: boolean
  user: { uuid: string; name: string } | null
}
interface Emits {
  (e: 'update:open', v: boolean): void
  (e: 'success', hash: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const file = ref<Blob | null>(null)
const cropKey = ref(0)
const { uploading, error, upload } = useImageUpload()

const onCrop = (blob: Blob) => {
  error.value = ''
  file.value = blob
}

const reset = () => {
  file.value = null
  error.value = ''
  cropKey.value++
}

const close = () => {
  if (uploading.value) return
  emit('update:open', false)
  setTimeout(reset, 200)
}

const submit = async () => {
  if (!file.value || !props.user || uploading.value) return
  const res = await upload<{ hash: string }>(
    `/admin/users/${props.user.uuid}/avatar`,
    file.value,
    'avatar.webp'
  )
  if (res) {
    emit('success', res.hash)
    close()
  }
}
</script>

<template>
  <KunModal :model-value="open" @update:model-value="close" aria-label="上传头像">
    <div class="space-y-4">
      <h2 class="text-foreground text-lg font-semibold">
        上传头像 — {{ user?.name }}
      </h2>

      <div class="space-y-3">
        <p class="text-default-500 text-sm">
          会推送到 image_service，自动生成 <code>_256</code> / <code>_100</code>
          变体并写入 <code>avatar_image_hash</code>。原 <code>avatar</code> URL
          保留作回退（老用户头像不会被覆盖）。
        </p>

        <CommonAvatarCrop :key="cropKey" :disabled="uploading" @crop="onCrop" />

        <div v-if="error" class="text-danger-600 text-sm">
          {{ error }}
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-2">
        <KunButton variant="flat" :disabled="uploading" @click="close">
          取消
        </KunButton>
        <KunButton
          color="primary"
          :disabled="!file || uploading"
          @click="submit"
        >
          <KunIcon
            v-if="uploading"
            name="lucide:loader-circle"
            class="mr-1 size-4 animate-spin"
          />
          上传
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
