<script setup lang="ts">
import type { DisplayLanguage } from '#shared/types/preferences'
import {
  COLOR_MODE_OPTIONS,
  DISPLAY_LANGUAGE_OPTIONS,
  DISPLAY_LANGUAGE_SAMPLE
} from '~/constants/preferences'

const colorMode = useColorMode()
const preferences = useEditorPreferences()

const displayLanguage = computed({
  get: () => preferences.displayLanguage.value,
  set: (value: DisplayLanguage) => preferences.setDisplayLanguage(value)
})
const colorModePreference = computed({
  get: () => colorMode.preference,
  set: (value: string) => {
    colorMode.preference = value
  }
})

const sample = computed(() => ({
  name: preferences.nameOf(DISPLAY_LANGUAGE_SAMPLE),
  other: preferences.otherNameOf(DISPLAY_LANGUAGE_SAMPLE)
}))

useHead({ title: '设置 · 未萌编辑台' })
</script>

<template>
  <div class="mx-auto max-w-3xl space-y-6">
    <KunHeader
      name="设置"
      description="编辑台的外观与显示偏好，保存在当前浏览器中。"
    />

    <section
      class="border-default-200 bg-content1 space-y-6 rounded-xl border p-5"
      aria-labelledby="settings-appearance"
    >
      <h2 id="settings-appearance" class="text-lg font-semibold">外观</h2>

      <div class="space-y-2">
        <div>
          <h3 class="text-sm font-medium">条目优先显示语言</h3>
          <p class="text-default-500 mt-1 text-sm">
            作品、角色、厂商等条目的名称在卡片、列表、搜索和编辑器里都按这里的语言优先显示；没有该语言的名称时显示原名。
          </p>
        </div>
        <KunRadioGroup
          v-model="displayLanguage"
          :options="DISPLAY_LANGUAGE_OPTIONS"
          variant="card"
          orientation="horizontal"
          aria-label="条目优先显示语言"
        />
        <div
          class="bg-default-100 flex items-center gap-3 rounded-lg px-3 py-2 text-sm"
        >
          <KunIcon name="lucide:eye" class="text-default-400 size-4 shrink-0" />
          <span class="text-default-500 shrink-0">示例</span>
          <span class="min-w-0 truncate font-medium">{{ sample.name }}</span>
          <span v-if="sample.other" class="text-default-400 min-w-0 truncate">
            {{ sample.other }}
          </span>
        </div>
      </div>

      <KunDivider />

      <div class="space-y-2">
        <h3 class="text-sm font-medium">主题</h3>
        <KunRadioGroup
          v-model="colorModePreference"
          :options="COLOR_MODE_OPTIONS"
          variant="card"
          orientation="horizontal"
          hide-indicator
          aria-label="主题"
        />
      </div>
    </section>
  </div>
</template>
