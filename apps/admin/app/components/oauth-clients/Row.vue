<script setup lang="ts">
import { REN_ONLY_SCOPES } from '~~/shared/types/oauth-client'

const props = defineProps<{
  client: OAuthClient
  sites: Site[]
}>()
const emit = defineEmits<{ edit: []; delete: [] }>()

const siteName = computed(() => {
  if (!props.client.site_id) return '未关联'
  const site = props.sites.find((s) => s.id === props.client.site_id)
  return site?.name ?? '未知站点'
})

const sensitiveScopes = computed(() =>
  (props.client.allowed_scopes ?? []).filter((s) => REN_ONLY_SCOPES.includes(s))
)
</script>

<template>
  <KunCard is-hoverable content-class="justify-start gap-0" class-name="p-4">
    <div class="flex items-center gap-4">
      <div
        class="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-default-200 bg-warning-100"
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
        <KunIcon v-else name="lucide:key" class="size-5 text-warning" />
      </div>

      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
          <h3 class="truncate font-semibold text-foreground">{{ client.name }}</h3>
          <span class="truncate text-sm text-default-400">{{ siteName }}</span>
          <KunChip
            :color="client.is_public ? 'info' : 'secondary'"
            variant="flat"
            size="sm"
          >
            {{ client.is_public ? '公共' : '机密' }}
          </KunChip>
          <KunChip v-if="client.auto_consent" color="warning" variant="flat" size="sm">
            自动同意
          </KunChip>
          <KunChip v-if="client.listed" color="success" variant="flat" size="sm">
            已展示
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

        <div class="mt-1 flex items-center gap-1 text-sm text-default-500">
          <span class="truncate font-mono text-xs">{{ client.id }}</span>
          <KunCopy :text="client.id" />
        </div>

        <p
          v-if="client.redirect_uris?.length"
          class="truncate text-xs text-default-400"
        >
          {{ client.redirect_uris.join('，') }}
        </p>
      </div>

      <div class="flex shrink-0 gap-1">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="编辑客户端"
          @click="emit('edit')"
        >
          <KunIcon name="lucide:pencil" class="size-5" />
        </KunButton>
        <KunButton
          variant="light"
          color="danger"
          size="sm"
          is-icon-only
          aria-label="删除客户端"
          @click="emit('delete')"
        >
          <KunIcon name="lucide:trash-2" class="size-5" />
        </KunButton>
      </div>
    </div>
  </KunCard>
</template>
