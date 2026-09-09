<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EcosystemApp } from '~~/shared/types/oauth-client'

const { data } = await useApiFetch<{ apps: EcosystemApp[] }>('/oauth/ecosystem')
const apps = computed(() => data.value?.apps ?? [])

const expanded = ref(false)

const initialOf = (name: string) => (name.trim()[0] ?? '?').toUpperCase()

const utmSource = useRequestURL().hostname
const linkFor = (app: EcosystemApp) => {
  if (!app.site_domain) return '#'
  const url = new URL(`https://${app.site_domain}`)
  url.searchParams.set('utm_source', utmSource)
  return url.toString()
}
</script>

<template>
  <div v-if="apps.length" class="mt-6 flex flex-col items-center gap-2">
    <p class="text-default-400 text-xs">
      拥有鲲 Galgame 账号，一键登录以下全部ACG网站
    </p>

    <div class="flex items-center justify-center gap-1">
      <a
        v-for="app in apps"
        :key="app.site_domain || app.name"
        :href="linkFor(app)"
        target="_blank"
        rel="noopener noreferrer"
        :title="app.tagline || app.name"
        class="border-default-200 bg-default-100 hover:border-primary flex size-6 shrink-0 items-center justify-center overflow-hidden rounded-full border transition-all hover:-translate-y-0.5"
      >
        <KunImage
          v-if="app.logo_url"
          :src="app.logo_url"
          :alt="app.name"
          :width="24"
          :height="24"
          object-fit="cover"
          class-name="size-full"
        />
        <span v-else class="text-default-500 text-[10px] font-bold">
          {{ initialOf(app.name) }}
        </span>
      </a>

      <button
        type="button"
        class="text-default-400 hover:bg-default-100 hover:text-default-600 ml-0.5 flex size-6 shrink-0 items-center justify-center rounded-full transition-colors"
        :aria-expanded="expanded"
        :aria-label="expanded ? '收起网站列表' : '展开网站列表'"
        @click="expanded = !expanded"
      >
        <KunIcon
          name="lucide:chevron-down"
          :class="
            cn(
              'size-4 transition-transform duration-300',
              expanded && 'rotate-180'
            )
          "
        />
      </button>
    </div>

    <div
      class="grid w-full transition-all duration-300 ease-out"
      :class="
        expanded ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0'
      "
    >
      <div class="overflow-hidden">
        <div class="scrollbar-hide flex max-h-[100px] flex-col gap-0.5 overflow-y-auto py-1">
          <a
            v-for="app in apps"
            :key="app.site_domain || app.name"
            :href="linkFor(app)"
            target="_blank"
            rel="noopener noreferrer"
            :title="app.tagline || app.name"
            class="group hover:bg-default-100 flex items-center gap-2 rounded-lg px-2 py-1.5 transition-colors"
          >
            <span
              class="border-default-200 bg-default-100 flex size-5 shrink-0 items-center justify-center overflow-hidden rounded-full border"
            >
              <KunImage
                v-if="app.logo_url"
                :src="app.logo_url"
                :alt="app.name"
                :width="20"
                :height="20"
                object-fit="cover"
                class-name="size-full"
              />
              <span v-else class="text-default-500 text-[9px] font-bold">
                {{ initialOf(app.name) }}
              </span>
            </span>
            <span
              class="text-default-500 group-hover:text-foreground min-w-0 flex-1 truncate text-xs"
            >
              {{ app.name }}
            </span>
            <KunChip
              v-if="app.auto_consent"
              color="primary"
              variant="flat"
              size="sm"
              class="shrink-0"
            >
              官方
            </KunChip>
          </a>
        </div>
      </div>
    </div>
  </div>
</template>
