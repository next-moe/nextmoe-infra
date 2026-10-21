<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { SPLIT_SOURCE_LABELS } from '~/constants/split'
import { releaseDateText } from '~/utils/merge/plan'
import { parseRefKey } from '~/utils/split/plan'

const open = defineModel<boolean>({ required: true })
const { preview, selection, draft } = useSplitWorkbench()

const releases = computed(() => preview.value?.releases ?? [])
const moving = (id: string) => selection.value.releases.includes(id)
const promoted = computed(() => selection.value.promote.map(parseRefKey))
</script>

<template>
  <KunModal
    v-model="open"
    title="拆分前后对比"
    description="左边是原作品现在的发行，划掉的会移到新作品；右边是新作品拆分后的样子。"
    size="xl"
  >
    <div v-if="preview" class="grid gap-4 md:grid-cols-2">
      <div class="border-default-200 space-y-2 rounded-xl border p-4">
        <p class="text-default-500 text-xs">原作品 · 拆分前 → 拆分后</p>
        <p class="font-semibold">{{ preview.target_name }}</p>
        <ul class="space-y-1 text-sm">
          <li
            v-for="r in releases"
            :key="r.id"
            :class="
              cn(
                'flex items-baseline gap-2',
                moving(r.id) && 'text-danger-600 line-through'
              )
            "
          >
            <span class="tabular-nums">{{ releaseDateText(r) }}</span>
            <span class="min-w-0 truncate">{{ r.title || '（无标题）' }}</span>
          </li>
        </ul>
      </div>
      <div
        class="border-primary-200 bg-primary-50/40 space-y-2 rounded-xl border p-4"
      >
        <p class="text-default-500 text-xs">新作品 · 拆分后</p>
        <p class="font-semibold">{{ draft.displayName || '（未命名）' }}</p>
        <ul class="space-y-1 text-sm">
          <li
            v-for="r in releases.filter((r) => moving(r.id))"
            :key="r.id"
            class="text-success-700 flex items-baseline gap-2"
          >
            <span class="tabular-nums">{{ releaseDateText(r) }}</span>
            <span class="min-w-0 truncate">{{ r.title || '（无标题）' }}</span>
          </li>
        </ul>
        <p class="text-default-500 pt-2 text-xs">
          标题：{{
            draft.titles
              .map((t) => t.title)
              .filter(Boolean)
              .join(' / ') || '无'
          }}
        </p>
        <p class="text-default-500 text-xs">
          正式对应：{{
            promoted
              .map(
                (r) =>
                  `${SPLIT_SOURCE_LABELS[r.source] ?? r.source} ${r.external_id}`
              )
              .join('、') || '无'
          }}
        </p>
      </div>
    </div>
  </KunModal>
</template>
