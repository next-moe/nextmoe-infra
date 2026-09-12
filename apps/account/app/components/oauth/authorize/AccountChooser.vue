<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'
import { roleColor, roleLabel, primaryRole, needsStepUp } from '~/constants/roles'

const props = defineProps<{
  sessions: BagSession[]
  clientName?: string
  switchingSub?: string | null
}>()

const emit = defineEmits<{
  pick: [sub: string]
  add: []
}>()

const cdnBase = useRuntimeConfig().public.imageCdnBase as string

const avatarSrc = (session: BagSession) =>
  resolveAvatarUrl(session, { cdnBase, variant: '100' }, '')

const isSwitching = computed(() => !!props.switchingSub)
</script>

<template>
  <div class="space-y-6">
    <div class="text-center">
      <div class="bg-primary-50 mx-auto mb-4 inline-flex size-14 items-center justify-center rounded-2xl">
        <KunIcon name="lucide:users" class="text-primary size-7" />
      </div>
      <h1 class="text-foreground text-xl font-bold">选择账号</h1>
      <p class="text-default-500 mt-2 text-sm">
        继续访问
        <span v-if="clientName" class="text-foreground font-medium">「{{ clientName }}」</span>
        <span v-else class="text-foreground font-medium">应用</span>
      </p>
    </div>

    <ul class="space-y-2">
      <li v-for="session in sessions" :key="session.sub">
        <button
          type="button"
          class="border-default-200 hover:border-primary-200 hover:bg-primary-50 flex w-full items-center gap-3 rounded-xl border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="isSwitching"
          @click="emit('pick', session.sub)"
        >
          <KunAvatar
            :user="{ id: 0, name: session.name, avatar: avatarSrc(session) }"
            size="md"
            :is-navigation="false"
          />
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <span class="text-foreground truncate font-medium">{{ session.name }}</span>
              <KunChip
                v-if="primaryRole(session.roles)"
                :color="roleColor(primaryRole(session.roles))"
                variant="flat"
                size="sm"
              >
                {{ roleLabel(primaryRole(session.roles)) }}
              </KunChip>
              <KunChip
                v-if="session.active"
                color="primary"
                variant="flat"
                size="sm"
              >
                当前
              </KunChip>
            </div>
            <p class="text-default-400 truncate text-sm">{{ session.email }}</p>
            <p
              v-if="!session.active && needsStepUp(session.roles)"
              class="text-warning text-xs"
            >
              切换管理员账号需要重新登录
            </p>
          </div>
          <KunIcon
            v-if="switchingSub === session.sub"
            name="lucide:loader-circle"
            class="text-primary size-5 shrink-0 animate-spin"
          />
          <KunIcon
            v-else
            name="lucide:chevron-right"
            class="text-default-300 size-5 shrink-0"
          />
        </button>
      </li>
    </ul>

    <KunDivider />

    <button
      type="button"
      class="hover:bg-default-100 flex w-full items-center gap-3 rounded-xl p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60"
      :disabled="isSwitching"
      @click="emit('add')"
    >
      <span class="border-default-200 text-default-400 flex size-10 shrink-0 items-center justify-center rounded-full border border-dashed">
        <KunIcon name="lucide:plus" class="size-5" />
      </span>
      <span class="text-default-500 font-medium">使用其他账号登录</span>
    </button>
  </div>
</template>
