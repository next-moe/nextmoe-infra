<script setup lang="ts">
import type { NsfwDisplay } from '~~/shared/types/preferences'
import {
  NSFW_DISPLAY_LABELS,
  NSFW_DISPLAY_OPTIONS
} from '~/constants/preferences'

const auth = useAuth()
const user = auth.user
const { setNsfwDisplay } = usePreferences()

const error = ref('')
const isSaving = ref(false)
const selected = ref<NsfwDisplay>('hide')

// nsfw_display rides only on /auth/me, so a store restored from the cookie of a
// session that logged in before this page existed has it undefined — read the
// account once rather than rendering someone else's default.
onMounted(async () => {
  if (!user.value?.nsfw_display) await auth.fetchUser()
})

watchEffect(() => {
  selected.value = user.value?.nsfw_display ?? 'hide'
})

const handleSelect = async (value: NsfwDisplay) => {
  if (isSaving.value || value === user.value?.nsfw_display) return
  error.value = ''
  isSaving.value = true
  try {
    const response = await setNsfwDisplay(value)
    if (response.code === 0) {
      useKunMessage(`已设为「${NSFW_DISPLAY_LABELS[value]}」`, 'success')
    } else {
      error.value = response.message || '保存失败'
      selected.value = user.value?.nsfw_display ?? 'hide'
    }
  } finally {
    isSaving.value = false
  }
}
</script>

<template>
  <KunCard class="p-6">
    <h3 class="text-foreground mb-4 text-lg font-semibold">
      <KunIcon name="lucide:shield-check" class="mr-2 inline size-5" />
      内容分级
    </h3>

    <KunRadioGroup
      v-model="selected"
      label="成人向内容的显示方式"
      variant="card"
      :options="NSFW_DISPLAY_OPTIONS"
      :disabled="isSaving"
      @change="handleSelect"
    />

    <p class="text-default-400 mt-3 text-xs">
      该选择跟着账号走，在所有接入 NextMoe 账号的站点上一致生效。
    </p>

    <div
      v-if="error"
      class="bg-danger-50 text-danger mt-4 rounded-lg p-3 text-sm"
    >
      {{ error }}
    </div>
  </KunCard>
</template>
