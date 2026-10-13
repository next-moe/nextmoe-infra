<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type {
  ExploreAttachRow,
  ExploreWorkBrief
} from '~~/shared/types/explore'

defineProps<{ rows: ExploreAttachRow[]; title: string; more: boolean }>()
const emit = defineEmits<{ go: [work: ExploreWorkBrief] }>()
</script>

<template>
  <div class="space-y-2">
    <h3 class="text-foreground text-sm font-semibold">
      {{ title }}（{{ rows.length
      }}<template v-if="more">，API 可翻页取更多</template>）
    </h3>
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <component
        :is="isBrowsableWork(r.work) ? 'button' : 'div'"
        v-for="(r, i) in rows"
        :key="`row-${i}`"
        :type="isBrowsableWork(r.work) ? 'button' : undefined"
        :class="
          cn(
            'border-default-200 flex w-full flex-col gap-1 rounded-lg border px-3 py-2 text-left',
            isBrowsableWork(r.work) && 'hover:border-primary transition-colors'
          )
        "
        @click="r.work && isBrowsableWork(r.work) && emit('go', r.work)"
      >
        <span class="flex w-full items-center gap-2">
          <span class="text-foreground min-w-0 flex-1 truncate text-sm">
            {{ r.work?.display_name ?? `#${r.work?.id}` }}
          </span>
          <KunChip
            v-if="r.work && !isBrowsableWork(r.work)"
            color="default"
            variant="flat"
            size="xs"
          >
            {{ vocabLabel('medium', r.work.medium) }}
          </KunChip>
          <KunChip
            v-if="r.work?.content_rating === 'r18'"
            color="danger"
            variant="flat"
            size="xs"
          >
            R18
          </KunChip>
        </span>
        <span
          v-if="r.roster_role || r.voices?.length || r.roles?.length"
          class="text-default-400 flex flex-wrap gap-x-2 text-xs"
        >
          <span v-if="r.roster_role">
            {{ vocabLabel('roster_role', r.roster_role) }}
          </span>
          <span v-if="r.voices?.length">
            CV {{ r.voices.map((v) => v.display_name).join(' / ') }}
          </span>
          <span v-if="r.roles?.length">
            {{ [...new Set(r.roles.map((ro) => ro.role_name))].join(' · ') }}
          </span>
        </span>
      </component>
    </div>
  </div>
</template>
