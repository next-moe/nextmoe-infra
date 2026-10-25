<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtSection } from '#shared/types/walkthrough'
import { wtTone } from '~/constants/walkthrough'
import { characterTones } from '~/utils/walkthrough/doc'

const props = defineProps<{ section: WtSection }>()

const doc = useWalkthroughDocument()
const tones = computed(() => characterTones(doc.chart.value))

const update = (patch: Partial<WtSection>, coalesce?: string) =>
  doc.edit((d) => {
    const s = d.sections.find((x) => x.key === props.section.key)
    if (s) Object.assign(s, patch)
  }, coalesce)

const issues = computed(() =>
  doc.issuesOf({ kind: 'section', key: props.section.key })
)
</script>

<template>
  <div :data-wt-section="props.section.key" class="space-y-1 pt-4 pb-1">
    <div class="flex items-center gap-2">
      <KunIcon
        name="lucide:bookmark"
        class="text-default-400 size-4 shrink-0"
      />
      <div class="min-w-0 flex-[3]">
        <KunInput
          :model-value="props.section.title"
          size="sm"
          placeholder="分节标题"
          :disabled="doc.readonly.value"
          :is-invalid="issues.length > 0"
          class-name="border-transparent bg-transparent shadow-none px-2 text-base font-semibold hover:border-default-200 focus:border-default-300"
          @update:model-value="
            update({ title: String($event) }, `section:${props.section.key}`)
          "
          @blur="doc.breakCoalescing"
        />
      </div>
      <div v-if="doc.lang.value === 'ja'" class="min-w-0 flex-[2]">
        <KunInput
          :model-value="props.section.title_zh ?? ''"
          size="sm"
          placeholder="中文标题（可选）"
          :disabled="doc.readonly.value"
          class-name="border-transparent bg-transparent shadow-none px-2 text-default-500 hover:border-default-200 focus:border-default-300"
          @update:model-value="
            update(
              { title_zh: String($event) || null },
              `section-zh:${props.section.key}`
            )
          "
          @blur="doc.breakCoalescing"
        />
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-1 pl-6">
      <span
        v-for="key in props.section.characters"
        :key="key"
        :class="
          cn(
            'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px]',
            wtTone(tones.get(key) ?? null).chip
          )
        "
      >
        {{ doc.chart.value.characters.find((c) => c.key === key)?.name || key }}
      </span>
      <button
        type="button"
        tabindex="-1"
        class="text-default-400 hover:bg-default-100 hover:text-default-600 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px]"
        @click="doc.select({ kind: 'section', key: props.section.key }, true)"
      >
        <KunIcon name="lucide:settings-2" class="size-3" />
        分节设置
      </button>
    </div>
  </div>
</template>
