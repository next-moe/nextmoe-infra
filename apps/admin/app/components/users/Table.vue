<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'
import { roleColor } from '~/constants/roles'

const props = defineProps<{ users: User[] }>()
const emit = defineEmits<{
  ban: [uuid: string]
  unban: [uuid: string]
  anonymize: [user: { uuid: string; name: string }]
  deleteSessions: [uuid: string]
  uploadAvatar: [user: { uuid: string; name: string }]
  moemoepoint: [user: { uuid: string; name: string; moemoepoint: number }]
  roles: [user: { uuid: string; name: string; roles: string[] }]
  siteRoles: [user: { uuid: string; name: string }]
  detail: [user: { uuid: string; name: string }]
  edit: [user: { uuid: string; name: string }]
}>()

const cdnBase = useRuntimeConfig().public.imageCdnBase as string

const avatarSrc = (user: User) =>
  resolveAvatarUrl(user, { cdnBase, variant: '256' }, '')

// Mirrors the backend's adminProtected: ren is granted in the DB and carries no
// "admin" role row, so a bare 'admin' check offers the one account above admin
// none of the protection.
const isProtected = (user: User) =>
  !!user.roles?.some((r) => r === 'admin' || r === 'ren')
</script>

<template>
  <div class="overflow-x-auto rounded-xl bg-content1 shadow-sm">
    <div v-if="users.length === 0" class="py-12 text-center">
      <KunIcon name="lucide:users" class="mx-auto mb-4 size-12 text-default-200" />
      <p class="text-default-400">暂无匹配用户</p>
    </div>

    <table v-else class="w-full">
      <thead class="border-b border-default-200 bg-default-50">
        <tr>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            用户
          </th>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            邮箱
          </th>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            角色
          </th>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            萌萌点
          </th>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            状态
          </th>
          <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-default-400">
            注册时间
          </th>
          <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-default-400">
            操作
          </th>
        </tr>
      </thead>
      <tbody class="divide-y divide-default-200">
        <tr v-for="user in users" :key="user.uuid" class="hover:bg-default-100">
          <td class="whitespace-nowrap px-6 py-4">
            <div class="flex items-center gap-3">
              <KunAvatar
                :user="{ id: 0, name: user.name, avatar: avatarSrc(user) }"
                size="sm"
                :is-navigation="false"
              />
              <div class="flex items-baseline gap-1.5">
                <span class="font-medium text-foreground">{{ user.name }}</span>
                <span v-if="user.id" class="text-default-300 font-mono text-xs">
                  #{{ user.id }}
                </span>
              </div>
            </div>
          </td>
          <td class="whitespace-nowrap px-6 py-4 text-default-400">
            {{ user.email || '——' }}
            <div v-if="user.original_email" class="text-default-300 text-xs">
              原邮箱：{{ user.original_email }}
            </div>
          </td>
          <td class="whitespace-nowrap px-6 py-4">
            <div class="flex gap-1">
              <KunChip
                v-for="role in (user.roles || [])"
                :key="role"
                :color="roleColor(role)"
                variant="flat"
                size="sm"
              >
                {{ role }}
              </KunChip>
              <span v-if="!user.roles?.length" class="text-default-300 text-sm">user</span>
            </div>
          </td>
          <td class="whitespace-nowrap px-6 py-4 text-default-400">
            {{ user.moemoepoint }}
          </td>
          <td class="whitespace-nowrap px-6 py-4">
            <UsersStatusBadge :status="user.status" :is-anonymized="user.is_anonymized" />
          </td>
          <td class="whitespace-nowrap px-6 py-4 text-default-400">
            {{ new Date(user.created_at).toLocaleDateString('zh-CN') }}
          </td>
          <td class="whitespace-nowrap px-6 py-4 text-right">
            <KunPopover position="bottom-end">
              <template #trigger>
                <KunButton
                  variant="light"
                  size="sm"
                  is-icon-only
                  aria-label="更多操作"
                >
                  <KunIcon name="lucide:ellipsis" class="size-5" />
                </KunButton>
              </template>
              <div class="w-44 py-1">
                <button
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('detail', { uuid: user.uuid, name: user.name })"
                >
                  <KunIcon name="lucide:eye" class="size-4" />
                  查看详情
                </button>
                <button
                  v-if="!user.is_anonymized && !isProtected(user)"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('edit', { uuid: user.uuid, name: user.name })"
                >
                  <KunIcon name="lucide:user-pen" class="size-4" />
                  编辑资料
                </button>
                <button
                  v-if="user.status === 0 && !user.is_anonymized && !isProtected(user)"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-danger hover:bg-danger-50"
                  @click="emit('ban', user.uuid)"
                >
                  <KunIcon name="lucide:ban" class="size-4" />
                  封禁用户
                </button>
                <button
                  v-else-if="user.status === 1 && !user.is_anonymized"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-success hover:bg-success-50"
                  @click="emit('unban', user.uuid)"
                >
                  <KunIcon name="lucide:circle-check" class="size-4" />
                  解除封禁
                </button>

                <button
                  v-if="!user.is_anonymized && !isProtected(user)"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-danger hover:bg-danger-50"
                  @click="emit('anonymize', { uuid: user.uuid, name: user.name })"
                >
                  <KunIcon name="lucide:user-x" class="size-4" />
                  注销并匿名化
                </button>

                <button
                  v-if="!user.is_anonymized"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('moemoepoint', { uuid: user.uuid, name: user.name, moemoepoint: user.moemoepoint })"
                >
                  <KunIcon name="lucide:sparkles" class="size-4" />
                  萌萌点
                </button>
                <button
                  v-if="!user.is_anonymized"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('uploadAvatar', { uuid: user.uuid, name: user.name })"
                >
                  <KunIcon name="lucide:image-up" class="size-4" />
                  上传头像
                </button>
                <button
                  v-if="!user.is_anonymized"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('roles', { uuid: user.uuid, name: user.name, roles: user.roles || [] })"
                >
                  <KunIcon name="lucide:shield" class="size-4" />
                  管理角色
                </button>
                <button
                  v-if="!user.is_anonymized"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('siteRoles', { uuid: user.uuid, name: user.name })"
                >
                  <KunIcon name="lucide:map-pin" class="size-4" />
                  站点角色
                </button>
                <button
                  v-if="!isProtected(user)"
                  class="flex w-full items-center gap-2 px-3 py-2 text-sm text-default-500 hover:bg-default-100 hover:text-foreground"
                  @click="emit('deleteSessions', user.uuid)"
                >
                  <KunIcon name="lucide:log-out" class="size-4" />
                  清除会话
                </button>

                <p
                  v-if="user.is_anonymized"
                  class="text-default-400 px-3 py-2 text-xs"
                >
                  已注销 · 不可恢复
                </p>
                <p
                  v-else-if="isProtected(user)"
                  class="text-default-400 px-3 py-2 text-xs"
                >
                  管理员 · 受保护操作已禁用
                </p>
              </div>
            </KunPopover>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
