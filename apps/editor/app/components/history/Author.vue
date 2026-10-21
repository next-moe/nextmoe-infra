<script setup lang="ts">
import type { CatalogUserRef } from '#shared/types/catalog'
import { SITE_LABELS } from '~/constants/catalog'

const props = withDefaults(
  defineProps<{
    user?: CatalogUserRef | null
    uid?: string | number | null
    origin?: string | null
    site?: string | null
    size?: 'xs' | 'sm'
    linked?: boolean
  }>(),
  {
    user: null,
    uid: null,
    origin: null,
    site: null,
    size: 'xs',
    linked: true
  }
)

const { originLabel, profileUrl, entryOf } = useOriginSites()

const sourceLabel = computed(() =>
  props.origin
    ? originLabel(props.origin)
    : props.site
      ? labelOf(SITE_LABELS, props.site)
      : ''
)
const sourceHint = computed(() =>
  props.origin
    ? (entryOf(props.origin)?.description ?? '')
    : '这条记录早于来源站记录，显示的是它归属的站点'
)
const link = computed(() =>
  props.linked ? profileUrl(props.origin, props.uid) : null
)
</script>

<template>
  <span class="inline-flex min-w-0 flex-wrap items-center gap-x-1">
    <template v-if="sourceLabel">
      <span class="text-default-400" :title="sourceHint">
        来自 {{ sourceLabel }}
      </span>
      <span class="text-default-300" aria-hidden="true">·</span>
    </template>
    <a
      v-if="link"
      :href="link"
      target="_blank"
      rel="noopener noreferrer"
      :title="`在${sourceLabel}查看这位用户`"
      :aria-label="`${props.user?.name ?? `用户 #${props.uid}`}（在${sourceLabel}的主页）`"
      class="hover:text-primary rounded-sm transition-colors"
    >
      <HistoryUser :user="props.user" :uid="props.uid" :size="props.size" />
    </a>
    <HistoryUser
      v-else
      :user="props.user"
      :uid="props.uid"
      :size="props.size"
    />
  </span>
</template>
