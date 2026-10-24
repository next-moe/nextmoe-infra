<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { COLOR_MODE_OPTIONS } from '~/constants/preferences'
import { ROLE_LABELS } from '~/constants/roles'

const { session, settled, logout } = useEditorSession()
const colorMode = useColorMode()
const config = useRuntimeConfig()

const loggingOut = ref(false)

const user = computed(() => ({
  id: 0,
  name: session.value?.name ?? '',
  avatar: session.value?.avatar ?? ''
}))

const roleChips = computed(() => {
  const names = [
    ...(session.value?.roles ?? []),
    ...(session.value?.siteRoles ?? [])
  ]
  return [...new Set(names)]
    .filter((name) => name in ROLE_LABELS)
    .map((name) => ROLE_LABELS[name])
})

const colorModeOptions = COLOR_MODE_OPTIONS

const handleLogout = async () => {
  loggingOut.value = true
  try {
    await logout()
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <KunPopover position="bottom-end" aria-label="账号菜单">
    <template #trigger>
      <button
        type="button"
        class="hover:bg-default-100 flex items-center rounded-full p-0.5 transition-colors"
        aria-label="账号菜单"
      >
        <KunSkeleton v-if="!settled" variant="circle" height="2rem" />
        <KunAvatar v-else :user="user" size="sm" :is-navigation="false" />
      </button>
    </template>

    <div class="w-60 p-2">
      <div class="flex items-center gap-3 px-2 py-2">
        <KunAvatar :user="user" size="md" :is-navigation="false" />
        <div class="min-w-0">
          <p class="truncate font-medium">{{ user.name }}</p>
          <div v-if="roleChips.length" class="mt-1 flex flex-wrap gap-1">
            <KunChip
              v-for="label in roleChips"
              :key="label"
              size="xs"
              variant="flat"
              color="primary"
            >
              {{ label }}
            </KunChip>
          </div>
        </div>
      </div>

      <KunDivider class-name="my-1" />

      <a
        :href="`${config.public.oauthWebBase}/profile`"
        target="_blank"
        rel="noopener"
        class="text-default-600 hover:bg-default-100 hover:text-foreground flex items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors"
      >
        <KunIcon name="lucide:user-cog" class="size-4" />
        账号中心
      </a>
      <NuxtLink
        to="/settings"
        class="text-default-600 hover:bg-default-100 hover:text-foreground flex items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors"
      >
        <KunIcon name="lucide:settings" class="size-4" />
        设置
      </NuxtLink>

      <div class="px-2 pt-2 pb-1">
        <p class="text-default-400 mb-1 text-xs">外观</p>
        <div class="bg-default-100 flex rounded-lg p-0.5">
          <button
            v-for="option in colorModeOptions"
            :key="option.value"
            type="button"
            :aria-label="option.label"
            :class="
              cn(
                'flex flex-1 items-center justify-center rounded-md py-1 transition-colors',
                colorMode.preference === option.value
                  ? 'bg-content1 text-foreground shadow-sm'
                  : 'text-default-500 hover:text-foreground'
              )
            "
            @click="colorMode.preference = option.value"
          >
            <KunIcon :name="option.icon" class="size-4" />
          </button>
        </div>
      </div>

      <KunDivider class-name="my-1" />

      <button
        type="button"
        :disabled="loggingOut"
        class="text-danger-600 hover:bg-danger-50 flex w-full items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors disabled:opacity-60"
        @click="handleLogout"
      >
        <KunIcon name="lucide:log-out" class="size-4" />
        退出登录
      </button>
    </div>
  </KunPopover>
</template>
