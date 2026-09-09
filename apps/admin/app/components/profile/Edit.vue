<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'

const auth = useAuth()
const user = auth.user
const userStore = useUserStore()

const name = ref('')
const bio = ref('')
const error = ref('')
const success = ref('')
const isLoading = ref(false)

const cdnBase = useRuntimeConfig().public.imageCdnBase as string
const avatarSrc = computed(() =>
  resolveAvatarUrl(user.value, { cdnBase, variant: '256' }, '')
)

const cropKey = ref(0)
const {
  uploading: avatarUploading,
  error: avatarError,
  upload: uploadAvatar,
} = useImageUpload()

// Both writes land in the user store, so never let them overlap: a profile PATCH
// that started before the upload answers with the old hash and would undo it.
const handleAvatarCropped = async (blob: Blob) => {
  if (isLoading.value) return
  const res = await uploadAvatar<{ hash: string }>(
    '/auth/me/avatar',
    blob,
    'avatar.webp'
  )
  if (res && userStore.user) {
    userStore.setUser({ ...userStore.user, avatar_image_hash: res.hash })
    useKunMessage('头像已更新', 'success')
    cropKey.value++
  }
}

watchEffect(() => {
  if (!user.value) return
  name.value = user.value.name ?? ''
  bio.value = user.value.bio ?? ''
})

const dirty = computed(
  () =>
    !!user.value &&
    (name.value !== (user.value.name ?? '') ||
      bio.value !== (user.value.bio ?? ''))
)

const handleSubmit = async () => {
  if (isLoading.value || avatarUploading.value) return
  error.value = ''
  success.value = ''

  if (name.value.trim().length < 2 || name.value.trim().length > 17) {
    error.value = '用户名长度需为 2–17 个字符'
    return
  }
  if (bio.value.length > 107) {
    error.value = '个人简介不能超过 107 个字符'
    return
  }
  if (!dirty.value) {
    error.value = '没有任何改动'
    return
  }

  const payload: { name?: string; bio?: string } = {}
  if (name.value !== (user.value?.name ?? '')) payload.name = name.value.trim()
  if (bio.value !== (user.value?.bio ?? '')) payload.bio = bio.value

  isLoading.value = true
  try {
    const response = await auth.updateProfile(payload)
    if (response.code === 0) {
      success.value = '资料已更新'
    } else {
      error.value = response.message || '更新失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '更新失败'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunCard class="p-6">
    <h3 class="mb-4 text-lg font-semibold text-foreground">
      <KunIcon name="lucide:user-pen" class="mr-2 inline size-5" />
      编辑资料
    </h3>

    <div class="mb-4 space-y-2">
      <div class="flex items-center gap-4">
        <KunAvatar
          :user="{ id: 0, name: name || '用户', avatar: avatarSrc }"
          size="lg"
          :is-navigation="false"
        />
        <div class="flex-1">
          <p class="text-sm font-medium text-default-500">头像</p>
          <p class="text-xs text-default-400">
            裁剪确认后立即上传并生效，无需保存。
          </p>
        </div>
      </div>
      <CommonAvatarCrop
        :key="cropKey"
        :disabled="avatarUploading"
        @crop="handleAvatarCropped"
      />
      <p
        v-if="avatarUploading"
        class="flex items-center justify-center gap-1 text-xs text-default-400"
      >
        <KunIcon name="lucide:loader-circle" class="size-3 animate-spin" />
        上传中…
      </p>
      <p v-if="avatarError" class="text-center text-sm text-danger">
        {{ avatarError }}
      </p>
    </div>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <KunInput
        v-model="name"
        label="用户名"
        placeholder="2–17 个字符，全站唯一"
        required
        autocomplete="username"
      />

      <KunTextarea
        v-model="bio"
        label="个人简介"
        placeholder="一句话介绍自己（≤107 字）"
        :rows="3"
      />

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div v-if="success" class="rounded-lg bg-success-50 p-3 text-sm text-success">
        {{ success }}
      </div>

      <KunButton
        type="submit"
        color="primary"
        class="w-full"
        :disabled="isLoading || !dirty || avatarUploading"
      >
        <KunIcon
          v-if="isLoading"
          name="lucide:loader-circle"
          class="mr-2 size-4 animate-spin"
        />
        {{ isLoading ? '保存中...' : '保存修改' }}
      </KunButton>
    </form>
  </KunCard>
</template>
