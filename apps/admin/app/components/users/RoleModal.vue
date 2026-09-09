<script setup lang="ts">
import { roleColor } from '~/constants/roles'

const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{
  user: { uuid: string; name: string; roles: string[] } | null
}>()
const emit = defineEmits<{ success: [] }>()

const api = useApi()
const { isRen, isAdmin } = useAuth()

const MANAGEABLE_ROLES = ['user', 'creator', 'moderator', 'admin'] as const

const canManage = (role: string) => {
  if (isRen.value) return true // ren manages every manageable role
  if (isAdmin.value) return role === 'moderator' || role === 'creator'
  return false
}

const roles = ref<string[]>([])
watch(
  () => props.user,
  (u) => {
    roles.value = [...(u?.roles ?? [])]
  },
  { immediate: true }
)

const has = (role: string) => roles.value.includes(role)
const loadingRole = ref('')

const toggle = async (role: string) => {
  if (!props.user || !canManage(role)) return
  loadingRole.value = role
  try {
    const res = has(role)
      ? await api.delete<{ roles: string[] }>(
          `/admin/users/${props.user.uuid}/roles/${role}`
        )
      : await api.post<{ roles: string[] }>(
          `/admin/users/${props.user.uuid}/roles`,
          { role }
        )
    if (res.code === 0) {
      roles.value = res.data?.roles ?? roles.value
      useKunMessage('角色已更新', 'success')
      emit('success')
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    loadingRole.value = ''
  }
}
</script>

<template>
  <KunModal v-model="open" aria-label="管理角色">
    <div class="space-y-4">
      <div>
        <h2 class="text-xl font-bold text-foreground">管理角色</h2>
        <p class="text-default-500 mt-1 text-sm">
          用户 <span class="text-foreground font-semibold">{{ user?.name }}</span>
        </p>
      </div>

      <div class="space-y-2">
        <div
          v-for="role in MANAGEABLE_ROLES"
          :key="role"
          class="border-default-200 flex items-center justify-between rounded-lg border p-3"
        >
          <div class="flex items-center gap-2">
            <KunChip :color="roleColor(role)" variant="flat" size="sm">
              {{ role }}
            </KunChip>
            <span class="text-default-400 text-xs">
              {{ has(role) ? '已拥有' : '未拥有' }}
            </span>
          </div>
          <KunButton
            v-if="canManage(role)"
            :color="has(role) ? 'danger' : 'primary'"
            variant="flat"
            size="sm"
            :disabled="loadingRole === role"
            @click="toggle(role)"
          >
            <KunIcon
              v-if="loadingRole === role"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            {{ has(role) ? '移除' : '授予' }}
          </KunButton>
          <span v-else class="text-default-300 text-xs">无权限</span>
        </div>

        <div
          v-if="has('ren')"
          class="border-secondary-200 bg-secondary-50 flex items-center justify-between rounded-lg border p-3"
        >
          <KunChip color="secondary" variant="flat" size="sm">ren</KunChip>
          <span class="text-default-500 text-xs">莲 · 仅数据库管理，不可在此修改</span>
        </div>
      </div>

      <div class="flex justify-end">
        <KunButton color="default" variant="flat" @click="open = false">
          关闭
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
