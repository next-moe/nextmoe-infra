<script setup lang="ts">
import { roleColor } from '~/constants/roles'

const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ user: { uuid: string; name: string } | null }>()
const emit = defineEmits<{ success: [] }>()

const api = useApi()

interface Site {
  id: number
  name: string
  domain: string
}
interface SiteRole {
  site_id: number
  site_name: string
  role_name: string
  granted_by: number
  granted_at: string
  expires_at?: string | null
  note?: string
}

const sites = ref<Site[]>([])
const grants = ref<SiteRole[]>([])
const loading = ref(false)

const siteId = ref<number | ''>('') // '' = nothing selected (KunSelect value type)
const roleName = ref('')
const granting = ref(false)
const revoking = ref('') // `${site_id}:${role_name}` currently being revoked

const siteOptions = computed(() =>
  sites.value.map((s) => ({ value: s.id, label: `${s.name} (${s.domain})` }))
)

const QUICK_ROLES = ['moderator', 'creator']

const grantKey = (g: SiteRole) => `${g.site_id}:${g.role_name}`

const load = async () => {
  if (!props.user) return
  loading.value = true
  try {
    const [detail, siteList] = await Promise.all([
      api.get<{ site_roles: SiteRole[] }>(`/admin/users/${props.user.uuid}`),
      api.get<Site[]>('/sites'),
    ])
    grants.value = detail.data?.site_roles ?? []
    sites.value = siteList.data ?? []
  } finally {
    loading.value = false
  }
}

watch(
  () => [open.value, props.user?.uuid],
  ([isOpen]) => {
    if (isOpen && props.user) {
      siteId.value = ''
      roleName.value = ''
      load()
    }
  },
  { immediate: true }
)

const grant = async () => {
  if (!props.user || !siteId.value || !roleName.value.trim()) return
  granting.value = true
  try {
    const res = await api.post(`/admin/users/${props.user.uuid}/site-roles`, {
      site_id: Number(siteId.value),
      role_name: roleName.value.trim(),
    })
    if (res.code === 0) {
      useKunMessage('站点角色已授予', 'success')
      roleName.value = ''
      await load()
      emit('success')
    } else {
      useKunMessage(res.message || '授予失败', 'error')
    }
  } finally {
    granting.value = false
  }
}

const revoke = async (g: SiteRole) => {
  if (!props.user) return
  revoking.value = grantKey(g)
  try {
    const res = await api.delete(
      `/admin/users/${props.user.uuid}/site-roles?site_id=${g.site_id}&role_name=${encodeURIComponent(g.role_name)}`
    )
    if (res.code === 0) {
      useKunMessage('站点角色已撤销', 'success')
      await load()
      emit('success')
    } else {
      useKunMessage(res.message || '撤销失败', 'error')
    }
  } finally {
    revoking.value = ''
  }
}
</script>

<template>
  <KunModal v-model="open" aria-label="站点角色">
    <div class="space-y-4">
      <div>
        <h2 class="text-foreground text-xl font-bold">站点角色</h2>
        <p class="text-default-500 mt-1 text-sm">
          用户
          <span class="text-foreground font-semibold">{{ user?.name }}</span>
          —— 仅在所选站点生效
        </p>
      </div>

      <div class="border-default-200 space-y-3 rounded-lg border p-3">
        <KunSelect
          v-model="siteId"
          label="站点"
          placeholder="请选择站点"
          :options="siteOptions"
        />
        <div class="space-y-1">
          <KunInput
            v-model="roleName"
            label="角色名"
            placeholder="如 moderator / event_organizer"
          />
          <div class="flex gap-1">
            <KunChip
              v-for="r in QUICK_ROLES"
              :key="r"
              :color="roleColor(r)"
              variant="flat"
              size="sm"
              class="cursor-pointer"
              @click="roleName = r"
            >
              {{ r }}
            </KunChip>
          </div>
        </div>
        <div class="flex justify-end">
          <KunButton
            color="primary"
            variant="flat"
            size="sm"
            :disabled="!siteId || !roleName.trim() || granting"
            @click="grant"
          >
            <KunIcon
              v-if="granting"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            授予
          </KunButton>
        </div>
      </div>

      <div class="space-y-2">
        <p class="text-default-400 text-xs">当前授予</p>
        <p v-if="loading" class="text-default-400 text-sm">加载中…</p>
        <p v-else-if="!grants.length" class="text-default-400 text-sm">
          暂无站点角色
        </p>
        <div
          v-for="g in grants"
          :key="grantKey(g)"
          class="border-default-200 flex items-center justify-between rounded-lg border p-3"
        >
          <div class="flex items-center gap-2">
            <KunChip :color="roleColor(g.role_name)" variant="flat" size="sm">
              {{ g.role_name }}
            </KunChip>
            <span class="text-default-500 text-sm">
              {{ g.site_name || `站点 #${g.site_id}` }}
            </span>
            <span v-if="g.expires_at" class="text-warning-500 text-xs">
              至 {{ g.expires_at.slice(0, 10) }}
            </span>
          </div>
          <KunButton
            color="danger"
            variant="flat"
            size="sm"
            :disabled="revoking === grantKey(g)"
            @click="revoke(g)"
          >
            <KunIcon
              v-if="revoking === grantKey(g)"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            撤销
          </KunButton>
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
