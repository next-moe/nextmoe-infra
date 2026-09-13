<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'
import { roleColor, roleLabel } from '~/constants/roles'

const auth = useAuth()
const user = auth.user

const cdnBase = useRuntimeConfig().public.imageCdnBase as string
const avatarSrc = computed(() =>
  resolveAvatarUrl(user.value ?? null, { cdnBase, variant: '256' }, '')
)

const formattedDate = computed(() => {
  if (!user.value?.created_at) return ''
  return new Date(user.value.created_at).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric'
  })
})
</script>

<template>
  <KunCard v-if="user" class="p-6">
    <div class="flex flex-col items-center text-center">
      <KunAvatar
        :user="{ id: 0, name: user.name, avatar: avatarSrc }"
        size="lg"
        :is-navigation="false"
      />

      <h2 class="text-foreground mt-4 text-lg font-semibold">{{ user.name }}</h2>
      <p class="text-default-400 mt-1 text-sm break-all">{{ user.email }}</p>

      <div v-if="user.roles?.length" class="mt-4 flex flex-wrap justify-center gap-2">
        <KunChip
          v-for="role in user.roles"
          :key="role"
          :color="roleColor(role)"
          size="sm"
        >
          {{ roleLabel(role) }}
        </KunChip>
      </div>

      <p v-if="user.bio" class="text-default-500 mt-4 text-sm leading-relaxed">
        {{ user.bio }}
      </p>
    </div>

    <div class="border-default-200 mt-6 space-y-3 border-t pt-5 text-sm">
      <div class="flex items-center justify-between gap-3">
        <span class="text-default-400 flex items-center gap-1.5">
          <KunIcon name="lucide:star" class="size-4" />
          萌萌点
        </span>
        <span class="text-foreground font-medium">{{ user.moemoepoint }}</span>
      </div>
      <div v-if="formattedDate" class="flex items-center justify-between gap-3">
        <span class="text-default-400 flex items-center gap-1.5">
          <KunIcon name="lucide:calendar" class="size-4" />
          注册于
        </span>
        <span class="text-foreground">{{ formattedDate }}</span>
      </div>
    </div>
  </KunCard>
</template>
