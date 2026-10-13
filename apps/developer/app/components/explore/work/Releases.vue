<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { EXPLORE_SOURCE_URL } from '~/constants/explore'
import type { KunTabItem } from '@kungal/ui-vue'
import type { ExploreRelease } from '~~/shared/types/explore'

const props = defineProps<{ releases: ExploreRelease[] }>()

const PAGE = 20

const kind = ref('all')
const expanded = ref(false)

const sorted = computed(() =>
  [...props.releases].sort((a, b) =>
    (a.date ?? '9999').localeCompare(b.date ?? '9999')
  )
)

const kinds = computed<KunTabItem[]>(() => {
  const count = new Map<string, number>()
  for (const r of props.releases) {
    const k = r.release_kind ?? 'default'
    count.set(k, (count.get(k) ?? 0) + 1)
  }
  return [
    { value: 'all', textValue: `全部 ${props.releases.length}` },
    ...[...count.entries()].map(([k, n]) => ({
      value: k,
      textValue: `${vocabLabel('release_kind', k)} ${n}`
    }))
  ]
})

const filtered = computed(() =>
  kind.value === 'all'
    ? sorted.value
    : sorted.value.filter((r) => (r.release_kind ?? 'default') === kind.value)
)
const visible = computed(() =>
  expanded.value ? filtered.value : filtered.value.slice(0, PAGE)
)

watch(kind, () => {
  expanded.value = false
})

const refLinks = (r: ExploreRelease) =>
  (r.refs ?? []).flatMap((x) => {
    const url = EXPLORE_SOURCE_URL[x.source]?.(x.external_id)
    return url ? [{ url, label: vocabLabel('sources', x.source) }] : []
  })

const KIND_COLOR: Record<
  string,
  'default' | 'primary' | 'warning' | 'secondary'
> = {
  digital: 'primary',
  physical: 'secondary',
  trial: 'warning',
  patch: 'default'
}
</script>

<template>
  <div class="space-y-4">
    <KunTab
      v-if="kinds.length > 2"
      v-model="kind"
      :items="kinds"
      variant="pills"
      size="sm"
      aria-label="按发行类型筛选"
    />

    <KunCard padding="none" class-name="overflow-hidden">
      <div
        v-for="r in visible"
        :key="r.id"
        class="border-default-100 grid grid-cols-1 gap-x-4 gap-y-1 border-b px-4 py-3 text-sm last:border-b-0 md:grid-cols-[6.5rem_minmax(0,1fr)_auto] md:items-center"
      >
        <span class="text-default-400 font-mono text-xs tabular-nums">
          {{ r.date ?? '日期未定' }}
        </span>
        <div class="min-w-0 space-y-1">
          <p
            :class="
              cn('truncate', r.title ? 'text-foreground' : 'text-default-400')
            "
          >
            {{ r.title ?? '未记录版本名' }}
          </p>
          <p
            class="text-default-500 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs"
          >
            <span v-if="r.lang" class="inline-flex items-center gap-1">
              <KunIcon name="lucide:languages" class="size-3" />
              {{ vocabLabel('release_lang', r.lang) }}
            </span>
            <span
              v-if="r.platforms?.length"
              class="inline-flex items-center gap-1"
            >
              <KunIcon name="lucide:monitor-smartphone" class="size-3" />
              {{
                r.platforms.map((p) => vocabLabel('platform', p)).join(' / ')
              }}
            </span>
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-1.5 md:justify-end">
          <KunChip
            v-if="r.release_kind && r.release_kind !== 'default'"
            :color="KIND_COLOR[r.release_kind] ?? 'default'"
            variant="flat"
            size="xs"
          >
            {{ vocabLabel('release_kind', r.release_kind) }}
          </KunChip>
          <KunLink
            v-for="l in refLinks(r)"
            :key="l.url"
            :href="l.url"
            target="_blank"
            size="sm"
            underline="hover"
            color="default"
            class-name="text-xs"
          >
            {{ l.label }}
          </KunLink>
        </div>
      </div>
    </KunCard>

    <div v-if="filtered.length > PAGE" class="flex justify-center">
      <KunButton
        variant="flat"
        color="default"
        size="sm"
        @click="expanded = !expanded"
      >
        {{ expanded ? '收起' : `显示全部 ${filtered.length} 个版本` }}
      </KunButton>
    </div>
  </div>
</template>
