<script setup lang="ts">
import { REN_ONLY_SCOPES } from '~~/shared/types/oauth-client'
import { IMAGE_PRESET_LABELS } from '~/constants/oauth-client'

const props = defineProps<{ client: OAuthClient }>()
const emit = defineEmits<{ edit: []; storage: []; delete: [] }>()

const { isRen } = useAuth()

const sensitiveScopes = computed(() =>
  (props.client.allowed_scopes ?? []).filter((s) => REN_ONLY_SCOPES.includes(s))
)

const imageLabel = computed(() => {
  const s = props.client.storage
  if (!s?.image_enabled) return ''
  const presets = (s.image_allowed_presets ?? []).map(
    (p) => IMAGE_PRESET_LABELS[p] ?? p
  )
  return `图片 · ${s.image_site_key || '未设 site_key'}${presets.length ? ` · ${presets.join('、')}` : ''}`
})

const artifactLabel = computed(() => {
  const s = props.client.storage
  if (!s?.artifact_enabled) return ''
  return `文件 · ${s.artifact_site_key || '未设 site_key'}`
})

const ownerLabel = computed(() => {
  const c = props.client
  if (c.mine) return ''
  if (c.owner_user_id) return `开发者 #${c.owner_user_id}`
  if (c.created_by_user_id) return `创建者 #${c.created_by_user_id}`
  return ''
})
</script>

<template>
  <KunCard content-class="justify-start gap-0" class-name="p-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:gap-4">
      <div class="flex min-w-0 flex-1 items-start gap-4">
        <div
          class="border-default-200 bg-default-100 flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg border"
        >
          <KunImage
            v-if="client.logo_url"
            :src="client.logo_url"
            :alt="client.name"
            :width="40"
            :height="40"
            object-fit="cover"
            class-name="size-full"
          />
          <KunIcon v-else name="lucide:key" class="text-default-500 size-5" />
        </div>

        <div class="min-w-0 flex-1 space-y-1">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 class="text-foreground truncate font-semibold">
              {{ client.name }}
            </h3>
            <KunChip
              :color="client.is_public ? 'info' : 'default'"
              variant="flat"
              size="sm"
            >
              {{ client.is_public ? '公共' : '机密' }}
            </KunChip>
            <KunChip
              v-if="client.auto_consent"
              color="warning"
              variant="flat"
              size="sm"
            >
              自动同意
            </KunChip>
            <KunChip
              v-if="client.listed"
              color="success"
              variant="flat"
              size="sm"
            >
              目录展示
            </KunChip>
          </div>

          <div
            class="text-default-400 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs"
          >
            <KunCopy :text="client.id" size="xs" class-name="font-mono" />
            <span v-if="ownerLabel">{{ ownerLabel }}</span>
          </div>

          <div
            v-if="
              imageLabel ||
              artifactLabel ||
              client.dev_enabled ||
              sensitiveScopes.length
            "
            class="flex flex-wrap gap-1.5 pt-1"
          >
            <KunChip v-if="imageLabel" color="primary" variant="flat" size="sm">
              <KunIcon name="lucide:image" class="mr-1 size-3.5" />{{
                imageLabel
              }}
            </KunChip>
            <KunChip
              v-if="artifactLabel"
              color="primary"
              variant="flat"
              size="sm"
            >
              <KunIcon name="lucide:package" class="mr-1 size-3.5" />{{
                artifactLabel
              }}
            </KunChip>
            <KunChip
              v-if="client.dev_enabled"
              color="secondary"
              variant="flat"
              size="sm"
            >
              <KunIcon name="lucide:terminal" class="mr-1 size-3.5" />开放 API
            </KunChip>
            <KunChip
              v-for="scope in sensitiveScopes"
              :key="scope"
              color="warning"
              variant="flat"
              size="sm"
            >
              {{ scope }}
            </KunChip>
          </div>

          <p
            v-if="client.redirect_uris?.length"
            class="text-default-400 truncate text-xs"
          >
            {{ client.redirect_uris.join('，') }}
          </p>
        </div>
      </div>

      <div class="-ml-2 flex shrink-0 gap-1 sm:ml-0">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="编辑登录配置"
          title="编辑登录配置"
          @click="emit('edit')"
        >
          <KunIcon name="lucide:pencil" class="size-5" />
        </KunButton>
        <KunButton
          v-if="isRen"
          variant="light"
          size="sm"
          is-icon-only
          aria-label="存储能力"
          title="存储能力（图片 / 文件上传）"
          @click="emit('storage')"
        >
          <KunIcon name="lucide:hard-drive" class="size-5" />
        </KunButton>
        <KunButton
          v-if="client.dev_enabled"
          variant="light"
          size="sm"
          is-icon-only
          aria-label="开放 API 与密钥"
          title="开放 API 与密钥"
          @click="navigateTo(`/devapi/${client.id}`)"
        >
          <KunIcon name="lucide:terminal" class="size-5" />
        </KunButton>
        <KunButton
          variant="light"
          color="danger"
          size="sm"
          is-icon-only
          aria-label="删除客户端"
          title="删除客户端"
          @click="emit('delete')"
        >
          <KunIcon name="lucide:trash-2" class="size-5" />
        </KunButton>
      </div>
    </div>
  </KunCard>
</template>
