<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { MergeSectionDef, MergeSectionKey } from '~/constants/merge'
import { MERGE_USER_COUNT_KEYS } from '~/constants/merge'
import {
  countAfter,
  demotedAnchors,
  duplicateReleases,
  mergedTitles
} from '~/utils/merge/plan'

const props = defineProps<{
  sections: MergeSectionDef[]
  active: MergeSectionKey
}>()
const emit = defineEmits<{ jump: [key: MergeSectionKey] }>()

const { preview, previewLoading, request, isReviewer } = useMergeWorkbench()

const back = computed(() =>
  !request.value
    ? { to: '/', label: '返回工作台' }
    : isReviewer.value
      ? { to: '/review/requests', label: '返回请求列表' }
      : { to: '/me/merge-requests', label: '返回我的合并请求' }
)

interface Badge {
  count?: number | null
  warn?: number
}

const badges = computed((): Record<string, Badge> => {
  const p = preview.value
  if (!p) {
    return {}
  }
  const userKeys = new Set(MERGE_USER_COUNT_KEYS)
  return {
    overview: { warn: p.blockers.length + p.warnings.length },
    fields: { count: p.fields.length },
    titles: { count: mergedTitles(p.titles).length },
    intros: { count: p.intros.length },
    releases: {
      count: p.releases.length,
      warn: duplicateReleases(p).length
    },
    anchors: {
      count: p.anchors.filter((a) => a.fate !== 'duplicate').length,
      warn: demotedAnchors(p).length
    },
    related: {
      count: p.counts
        .filter((c) => !userKeys.has(c.key))
        .reduce((sum, c) => sum + countAfter(c), 0)
    },
    users: {
      count: p.counts
        .filter((c) => userKeys.has(c.key))
        .reduce((sum, c) => sum + c.on_source, 0)
    },
    claim: {
      warn: p.claim?.fate === 'dropped' ? 1 : 0
    }
  }
})
</script>

<template>
  <nav class="flex h-full min-h-0 flex-col" aria-label="合并分区">
    <div class="shrink-0 px-2 pt-2">
      <NuxtLink
        :to="back.to"
        class="text-default-600 hover:bg-default-100 hover:text-foreground flex h-9 items-center gap-2 rounded-lg px-2.5 text-sm transition-colors"
      >
        <KunIcon name="lucide:arrow-left" class="size-4 shrink-0" />
        {{ back.label }}
      </NuxtLink>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto px-2 py-3">
      <p class="text-default-400 px-2.5 pb-1 text-xs font-medium">合并</p>
      <ul class="space-y-0.5">
        <li v-for="section in props.sections" :key="section.key">
          <button
            type="button"
            :aria-current="props.active === section.key ? 'true' : undefined"
            :class="
              cn(
                'flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm transition-colors',
                props.active === section.key
                  ? 'bg-primary-50 text-primary-700 font-medium'
                  : 'text-default-600 hover:bg-default-100 hover:text-foreground'
              )
            "
            @click="emit('jump', section.key)"
          >
            <KunIcon :name="section.icon" class="size-4 shrink-0" />
            <span class="min-w-0 flex-1 truncate">{{ section.label }}</span>
            <span
              v-if="badges[section.key]?.warn"
              class="bg-warning-100 text-warning-700 rounded-full px-1.5 text-xs leading-5 tabular-nums"
              :aria-label="`${badges[section.key]!.warn} 项需要注意`"
            >
              {{ badges[section.key]!.warn }}
            </span>
            <span
              v-else-if="badges[section.key]?.count != null"
              class="text-default-400 text-xs tabular-nums"
            >
              {{ badges[section.key]!.count }}
            </span>
          </button>
        </li>
      </ul>
      <div v-if="previewLoading && !preview" class="space-y-2 px-2.5 pt-3">
        <KunSkeleton v-for="n in 6" :key="n" variant="text" width="70%" />
      </div>
    </div>

    <div class="border-default-200 shrink-0 border-t p-2">
      <a
        href="/guidelines/create-and-merge#merge-requests"
        target="_blank"
        rel="noopener"
        class="text-default-600 hover:bg-default-100 hover:text-foreground flex h-9 items-center gap-2 rounded-lg px-2.5 text-sm transition-colors"
      >
        <KunIcon name="lucide:book-open" class="size-4 shrink-0" />
        合并规范
      </a>
    </div>
  </nav>
</template>
