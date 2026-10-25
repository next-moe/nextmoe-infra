<script setup lang="ts">
import type { CatalogWorkCharacter } from '#shared/types/catalog'
import type { WtLang } from '#shared/types/walkthrough'
import { WT_LANGS } from '~/constants/walkthrough'
import { stripTranslations } from '~/utils/walkthrough/doc'

const props = defineProps<{
  roster: CatalogWorkCharacter[]
  rosterLoaded: boolean
  hideable: boolean
}>()

const doc = useWalkthroughDocument()

const hasTranslations = () =>
  doc.chart.value.segments.some(
    (s) =>
      s.condition_zh ||
      s.steps.some((st) => st.text_zh || st.alt_zh.some(Boolean))
  ) ||
  doc.chart.value.endings.some((e) => e.name_zh) ||
  doc.chart.value.sections.some((s) => s.title_zh)

const setLang = async (lang: WtLang) => {
  if (lang === doc.lang.value) return
  if (lang === 'zh-Hans' && hasTranslations()) {
    const ok = await useKunAlert({
      title: '改为中文攻略',
      message: '中文攻略不再需要译文，现有的中文译文会被清除。这一步可以撤销。',
      type: 'warning',
      confirmText: '清除译文并切换',
      cancelText: '取消'
    })
    if (!ok) return
    doc.edit(stripTranslations)
  }
  doc.setLang(lang)
}

watch(
  () => doc.revealTick.value,
  () => {
    const target = doc.selection.value
    if (target && target.kind !== 'segment') {
      doc.expanded.value = `${target.kind}:${target.key}`
    }
  }
)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="min-h-0 flex-1 space-y-5 overflow-y-auto px-3 py-4">
      <section class="space-y-2">
        <h3 class="text-default-500 px-2 text-xs font-medium">攻略设置</h3>
        <KunSelect
          :model-value="doc.lang.value"
          :options="WT_LANGS"
          size="sm"
          label="攻略语言"
          :disabled="doc.readonly.value"
          @update:model-value="setLang($event as WtLang)"
        />
        <p class="text-default-400 px-1 text-xs">
          {{
            doc.lang.value === 'ja'
              ? '选项写日文原文，可以在下面附上中文译文。'
              : '直接用中文写选项，不需要译文。'
          }}
        </p>
        <div v-if="doc.setWorkId" class="space-y-1">
          <p class="text-default-700 text-sm font-medium">所属作品</p>
          <EditRefPicker
            :model-value="doc.workId.value"
            family="work"
            aria-label="所属作品"
            placeholder="搜索作品名或输入 ID"
            :disabled="doc.readonly.value"
            @update:model-value="$event && doc.setWorkId?.($event)"
          />
          <p class="text-default-400 px-1 text-xs">挂错作品时在这里改。</p>
        </div>
        <KunSwitch
          v-if="props.hideable && !doc.readonly.value"
          :model-value="doc.hidden.value"
          size="sm"
          label="隐藏这篇攻略"
          @update:model-value="doc.setHidden(Boolean($event))"
        />
      </section>

      <WalkthroughsEditorRailCharacters
        :roster="props.roster"
        :roster-loaded="props.rosterLoaded"
      />
      <WalkthroughsEditorRailEndings />
      <WalkthroughsEditorRailLabels kind="save" />
      <WalkthroughsEditorRailLabels kind="entry" />
      <WalkthroughsEditorRailLabels kind="section" />
    </div>
  </div>
</template>
