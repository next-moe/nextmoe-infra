<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogMergePreviewAnchor } from '#shared/types/catalog'
import { MERGE_LINK_KIND_LABELS } from '~/constants/merge'
import { contextLinkUrl } from '~/utils/externalLinks'
import { mergedAnchors, sourceLabel } from '~/utils/merge/plan'

const { preview, family } = useMergeWorkbench()

const anchors = computed(() => preview.value?.anchors ?? [])
const bySide = (side: 'source' | 'target') =>
  anchors.value.filter((anchor) => anchor.side === side)
const result = computed(() => mergedAnchors(anchors.value))

const label = (anchor: CatalogMergePreviewAnchor) =>
  sourceLabel(anchor.source_key)
const url = (anchor: CatalogMergePreviewAnchor) =>
  contextLinkUrl(family.value, anchor.source_key, anchor.external_id)
const KIND_TONE: Record<string, string> = {
  identity: 'text-success-700',
  probable: 'text-warning-700',
  related: 'text-default-500'
}
</script>

<template>
  <MergeWorkbenchSection v-if="preview" section-key="anchors">
    <div class="grid gap-3 md:grid-cols-3">
      <MergeWorkbenchCell
        v-for="side in ['source', 'target'] as const"
        :key="side"
        :side="side"
      >
        <ul class="divide-default-100 divide-y px-3 py-1">
          <li
            v-for="anchor in bySide(side)"
            :key="`${anchor.source_key}:${anchor.external_id}`"
            class="space-y-1 py-2"
          >
            <div class="flex items-center gap-2 text-sm">
              <span class="font-medium">{{ label(anchor) }}</span>
              <a
                v-if="url(anchor)"
                :href="url(anchor)!"
                target="_blank"
                rel="noopener"
                class="hover:text-primary min-w-0 truncate"
              >
                {{ anchor.external_id }}
              </a>
              <span v-else class="min-w-0 truncate">{{
                anchor.external_id
              }}</span>
              <span class="flex-1" />
              <MergeWorkbenchFate
                v-if="side === 'source'"
                :fate="anchor.fate"
                :reason="anchor.reason"
              />
            </div>
            <p :class="cn('text-xs', KIND_TONE[anchor.kind_before])">
              {{ MERGE_LINK_KIND_LABELS[anchor.kind_before] }}
              <template v-if="anchor.kind_after !== anchor.kind_before">
                → {{ MERGE_LINK_KIND_LABELS[anchor.kind_after] }}
              </template>
            </p>
            <p
              v-if="side === 'source' && anchor.reason"
              class="text-default-500 text-xs"
            >
              {{ anchor.reason }}
            </p>
          </li>
          <li v-if="!bySide(side).length" class="text-default-400 py-3 text-sm">
            没有资料源对应
          </li>
        </ul>
      </MergeWorkbenchCell>
      <MergeWorkbenchCell side="result">
        <ul class="divide-primary-100 divide-y px-3 py-1">
          <li
            v-for="anchor in result"
            :key="`r:${anchor.source_key}:${anchor.external_id}`"
            class="flex items-center gap-2 py-2 text-sm"
          >
            <span
              :class="
                cn(
                  'size-1.5 shrink-0 rounded-full',
                  anchor.side === 'source' ? 'bg-success-500' : 'bg-transparent'
                )
              "
            />
            <span class="font-medium">{{ label(anchor) }}</span>
            <span class="min-w-0 flex-1 truncate">{{
              anchor.external_id
            }}</span>
            <span :class="cn('shrink-0 text-xs', KIND_TONE[anchor.kind_after])">
              {{ MERGE_LINK_KIND_LABELS[anchor.kind_after] }}
            </span>
          </li>
        </ul>
      </MergeWorkbenchCell>
    </div>
  </MergeWorkbenchSection>
</template>
