<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'
import { roleColor } from '~/constants/roles'

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
    day: 'numeric',
  })
})
</script>

<template>
  <KunCard v-if="user" class="p-6">
    <div class="flex items-start gap-6">
      <KunAvatar
        :user="{ id: 0, name: user.name, avatar: avatarSrc }"
        size="lg"
        :is-navigation="false"
      />

      <div class="flex-1 space-y-3">
        <div>
          <h2 class="text-xl font-semibold text-foreground">{{ user.name }}</h2>
          <p class="text-default-400 text-sm">{{ user.email }}</p>
        </div>

        <p v-if="user.bio" class="text-default-500 text-sm">{{ user.bio }}</p>

        <div class="flex flex-wrap items-center gap-4 text-sm">
          <span class="text-default-400 flex items-center gap-1">
            <KunIcon name="lucide:star" class="size-4" />
            萌萌点：{{ user.moemoepoint }}
          </span>
          <span class="text-default-400 flex items-center gap-1">
            <KunIcon name="lucide:calendar" class="size-4" />
            注册时间：{{ formattedDate }}
          </span>
        </div>

        <div v-if="user.roles?.length" class="flex gap-2">
          <KunChip
            v-for="role in user.roles"
            :key="role"
            :color="roleColor(role)"
          >
            {{ role }}
          </KunChip>
        </div>
      </div>
    </div>
  </KunCard>
</template>
