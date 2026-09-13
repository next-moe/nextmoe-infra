<script setup lang="ts">
import { resolveAvatarUrl } from '~~/shared/utils/resolveImage'
import { roleColor } from '~/constants/roles'
import { USER_STATUS_MAP } from '~/constants/admin'
import type { UserDetail } from '~~/shared/types/user'

const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ user: { uuid: string; name: string } | null }>()

const api = useApi()
const cdnBase = useRuntimeConfig().public.imageCdnBase as string

const detail = ref<UserDetail | null>(null)
const loading = ref(false)
const errored = ref(false)

const load = async () => {
  if (!props.user) return
  loading.value = true
  errored.value = false
  detail.value = null
  try {
    const res = await api.get<UserDetail>(`/admin/users/${props.user.uuid}`)
    if (res.code === 0 && res.data) {
      detail.value = res.data
    } else {
      errored.value = true
    }
  } finally {
    loading.value = false
  }
}

watch(open, (v) => {
  if (v && props.user) load()
})

const avatarSrc = computed(() =>
  detail.value
    ? resolveAvatarUrl(detail.value, { cdnBase, variant: '256' }, '')
    : ''
)
const siteStatusMeta = (s: number) =>
  USER_STATUS_MAP[s] ?? { label: String(s), color: 'default' as const }
const fmt = (s?: string | null) => (s ? new Date(s).toLocaleString('zh-CN') : '—')
</script>

<template>
  <KunDrawer
    v-model="open"
    placement="right"
    title="用户详情"
    inner-class-name="p-5"
  >
    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-6 animate-spin"
      />
    </div>
    <CommonFetchError v-else-if="errored" @retry="load" />
    <div v-else-if="detail" class="space-y-5">
      <div class="flex items-center gap-3">
        <KunAvatar
          :user="{ id: 0, name: detail.name, avatar: avatarSrc }"
          size="lg"
          :is-navigation="false"
        />
        <div class="min-w-0">
          <div class="flex items-baseline gap-2">
            <span class="text-foreground text-lg font-semibold">
              {{ detail.name }}
            </span>
            <span v-if="detail.id" class="text-default-400 font-mono text-sm">
              #{{ detail.id }}
            </span>
          </div>
          <div class="flex items-center gap-1">
            <span class="text-default-400 truncate font-mono text-xs">
              {{ detail.uuid }}
            </span>
            <KunCopy :text="detail.uuid" size="xs" />
          </div>
        </div>
      </div>

      <dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
        <div class="min-w-0">
          <dt class="text-default-400 text-xs">邮箱</dt>
          <dd class="text-foreground break-all">{{ detail.email || '——' }}</dd>
        </div>
        <div>
          <dt class="text-default-400 text-xs">状态</dt>
          <dd>
            <UsersStatusBadge
              :status="detail.status"
              :is-anonymized="detail.is_anonymized"
            />
          </dd>
        </div>
        <div>
          <dt class="text-default-400 text-xs">萌萌点</dt>
          <dd class="text-foreground">{{ detail.moemoepoint }}</dd>
        </div>
        <div>
          <dt class="text-default-400 text-xs">注册时间</dt>
          <dd class="text-foreground">{{ fmt(detail.created_at) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="text-default-400 text-xs">最近 IP</dt>
          <dd class="text-foreground font-mono break-all">
            {{ detail.ip || '——' }}
          </dd>
        </div>
        <div>
          <dt class="text-default-400 text-xs">活跃会话</dt>
          <dd class="text-foreground">{{ detail.session_count }}</dd>
        </div>
        <div>
          <dt class="text-default-400 text-xs">关联登录数</dt>
          <dd class="text-foreground">{{ detail.oauth_accounts }}</dd>
        </div>
        <div v-if="detail.original_email" class="min-w-0">
          <dt class="text-default-400 text-xs">原邮箱（注销前）</dt>
          <dd class="text-foreground break-all">{{ detail.original_email }}</dd>
        </div>
      </dl>

      <div v-if="detail.bio">
        <p class="text-default-400 mb-1 text-xs">简介</p>
        <p class="text-foreground text-sm whitespace-pre-line">
          {{ detail.bio }}
        </p>
      </div>

      <div>
        <p class="text-default-400 mb-1 text-xs">全局角色</p>
        <div class="flex flex-wrap gap-1">
          <KunChip
            v-for="r in detail.roles || []"
            :key="r"
            :color="roleColor(r)"
            variant="flat"
            size="sm"
          >
            {{ r }}
          </KunChip>
          <span v-if="!detail.roles?.length" class="text-default-300 text-sm">
            user
          </span>
        </div>
      </div>

      <div v-if="detail.site_data?.length">
        <p class="text-default-400 mb-1 text-xs">各站点状态</p>
        <div class="space-y-1">
          <div
            v-for="sd in detail.site_data"
            :key="sd.site_id"
            class="border-default-200 flex items-center justify-between rounded-lg border px-3 py-2 text-sm"
          >
            <span class="text-foreground">
              {{ sd.site_name || `站点 #${sd.site_id}` }}
            </span>
            <KunChip
              :color="siteStatusMeta(sd.status).color"
              variant="flat"
              size="xs"
            >
              {{ siteStatusMeta(sd.status).label }}
            </KunChip>
          </div>
        </div>
      </div>

      <div v-if="detail.site_roles?.length">
        <p class="text-default-400 mb-1 text-xs">站点角色</p>
        <div class="space-y-1">
          <div
            v-for="g in detail.site_roles"
            :key="`${g.site_id}:${g.role_name}`"
            class="border-default-200 flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-sm"
          >
            <KunChip :color="roleColor(g.role_name)" variant="flat" size="sm">
              {{ g.role_name }}
            </KunChip>
            <span class="text-default-500">
              {{ g.site_name || `站点 #${g.site_id}` }}
            </span>
            <span v-if="g.expires_at" class="text-warning-500 text-xs">
              至 {{ g.expires_at.slice(0, 10) }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </KunDrawer>
</template>
