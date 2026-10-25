<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { diffCharts, type WtChangeStatus } from '~/utils/walkthrough/diff'

const COLLAPSED_LIMIT = 12

const props = defineProps<{ before: unknown; after: unknown }>()

const STATUS: Record<WtChangeStatus, { label: string; tone: string }> = {
  added: { label: '新增', tone: 'bg-success-100 text-success-700' },
  removed: { label: '删除', tone: 'bg-danger-100 text-danger-700' },
  changed: { label: '修改', tone: 'bg-warning-100 text-warning-700' }
}

const changes = computed(() => diffCharts(props.before, props.after))
const expanded = ref(false)
const shown = computed(() =>
  expanded.value ? changes.value : changes.value.slice(0, COLLAPSED_LIMIT)
)
</script>

<template>
  <div class="space-y-2 text-sm">
    <p v-if="!changes.length" class="text-default-500">攻略图没有实质变化。</p>
    <ul class="space-y-1.5">
      <li
        v-for="change in shown"
        :key="`${change.group}:${change.key}`"
        class="border-default-200 rounded-md border px-3 py-2"
      >
        <div class="flex items-center gap-2">
          <span
            :class="
              cn(
                'rounded px-1.5 text-[11px] leading-5',
                STATUS[change.status].tone
              )
            "
          >
            {{ STATUS[change.status].label }}
          </span>
          <span class="text-default-500 shrink-0 text-xs">{{
            change.group
          }}</span>
          <span class="min-w-0 truncate font-medium">{{ change.label }}</span>
        </div>
        <ul
          v-if="change.details.length"
          class="text-default-600 mt-1 space-y-0.5 pl-1 text-xs"
        >
          <li v-for="(line, i) in change.details" :key="i" class="break-words">
            {{ line }}
          </li>
        </ul>
      </li>
    </ul>
    <KunButton
      v-if="changes.length > COLLAPSED_LIMIT"
      size="sm"
      variant="light"
      @click="expanded = !expanded"
    >
      {{ expanded ? '收起' : `展开全部 ${changes.length} 处变化` }}
    </KunButton>
  </div>
</template>
