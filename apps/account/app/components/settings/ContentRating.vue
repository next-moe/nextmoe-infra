<script setup lang="ts">
import type { NsfwDisplay } from '~~/shared/types/preferences'
import {
  ADULT_CONFIRMATION_COPY,
  NSFW_DISPLAY_LABELS,
  NSFW_DISPLAY_OPTIONS
} from '~/constants/preferences'

const auth = useAuth()
const user = auth.user
const { confirmAdult, setNsfwDisplay } = usePreferences()

const error = ref('')
const isConfirming = ref(false)
const isSaving = ref(false)
const showConfirmDialog = ref(false)

const isConfirmed = computed(() => !!user.value?.adult_confirmed_at)
const selected = ref<NsfwDisplay>('hide')

// The two fields ride only on /auth/me, so a store restored from the cookie of
// a session that logged in before this page existed has them undefined — read
// the account once rather than rendering someone else's default.
onMounted(async () => {
  if (!user.value?.nsfw_display) await auth.fetchUser()
})

watchEffect(() => {
  selected.value = user.value?.nsfw_display ?? 'hide'
})

const effective = computed(() =>
  effectiveNsfwDisplay(user.value?.adult_confirmed_at, user.value?.nsfw_display)
)

const confirmedAtText = computed(() =>
  user.value?.adult_confirmed_at
    ? formatPreferenceTime(user.value.adult_confirmed_at)
    : ''
)

const options = computed(() =>
  NSFW_DISPLAY_OPTIONS.map((o) => ({
    value: o.value,
    label: o.label,
    description: o.description,
    disabled: o.adultOnly && !isConfirmed.value
  }))
)

const handleConfirm = async () => {
  if (isConfirming.value) return
  error.value = ''
  isConfirming.value = true
  try {
    const response = await confirmAdult()
    if (response.code === 0) {
      showConfirmDialog.value = false
      useKunMessage('年龄确认已记录', 'success')
    } else {
      error.value = response.message || '确认失败'
    }
  } finally {
    isConfirming.value = false
  }
}

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

    <div
      v-if="isConfirmed"
      class="border-default-200 mb-5 flex items-start gap-3 rounded-lg border p-3"
    >
      <KunIcon
        name="lucide:user-check"
        class="text-success mt-0.5 size-5 shrink-0"
      />
      <div class="min-w-0 flex-1">
        <p class="text-foreground text-sm font-medium">已完成年龄确认</p>
        <p class="text-default-400 mt-1 text-xs">
          确认时间 {{ confirmedAtText }}，该记录不可撤销
        </p>
      </div>
    </div>

    <div
      v-else
      class="border-default-200 bg-warning-50 mb-5 flex items-start gap-3 rounded-lg border p-3"
    >
      <KunIcon
        name="lucide:triangle-alert"
        class="text-warning mt-0.5 size-5 shrink-0"
      />
      <div class="min-w-0 flex-1">
        <p class="text-foreground text-sm font-medium">尚未完成年龄确认</p>
        <p class="text-default-500 mt-1 text-xs">
          未确认前，成人向内容一律隐藏，「模糊」与「显示」不可选。
        </p>
        <KunButton
          color="primary"
          size="sm"
          class="mt-3"
          @click="showConfirmDialog = true"
        >
          进行年龄确认
        </KunButton>
      </div>
    </div>

    <KunRadioGroup
      v-model="selected"
      label="成人向内容的显示方式"
      variant="card"
      :options="options"
      :disabled="isSaving"
      @change="handleSelect"
    />

    <p class="text-default-400 mt-3 text-xs">
      当前实际效果：{{ NSFW_DISPLAY_LABELS[effective] }}
      <span v-if="!isConfirmed">（未完成年龄确认，选择暂不生效）</span>
    </p>

    <div
      v-if="error"
      class="bg-danger-50 text-danger mt-4 rounded-lg p-3 text-sm"
    >
      {{ error }}
    </div>

    <KunModal
      v-model="showConfirmDialog"
      :title="ADULT_CONFIRMATION_COPY.title"
      size="sm"
      role="alertdialog"
    >
      <div class="space-y-4">
        <p class="text-default-500 text-sm leading-relaxed">
          {{ ADULT_CONFIRMATION_COPY.body }}
        </p>
        <div class="flex justify-end gap-2">
          <KunButton
            variant="light"
            color="default"
            :disabled="isConfirming"
            @click="showConfirmDialog = false"
          >
            {{ ADULT_CONFIRMATION_COPY.cancel }}
          </KunButton>
          <KunButton
            color="primary"
            :loading="isConfirming"
            @click="handleConfirm"
          >
            {{ ADULT_CONFIRMATION_COPY.confirm }}
          </KunButton>
        </div>
      </div>
    </KunModal>
  </KunCard>
</template>
