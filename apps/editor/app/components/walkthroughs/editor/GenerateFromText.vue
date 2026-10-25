<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtChart } from '#shared/types/walkthrough'
import { normalizeChart } from '~/utils/walkthrough/doc'
import { problemMessage } from '~/utils/review/problem'

interface DraftReply {
  verdict: 'ok' | 'review' | 'rejected' | 'no_chart'
  chart: WtChart | null
  coverage: number
  missed: string[]
  not_verbatim: string[]
  remaining: number
}

const MAX_CHARS = 60000
const KANA = /[\u3040-\u30ff]/

const open = defineModel<boolean>({ default: false })

const api = useEditorApi()
const doc = useWalkthroughDocument()

const text = ref('')
const loading = ref(false)
const reply = ref<DraftReply | null>(null)

const VERDICT: Record<
  DraftReply['verdict'],
  { label: string; tone: string; hint: string }
> = {
  ok: {
    label: '提取完整',
    tone: 'bg-success-100 text-success-700',
    hint: '可以直接载入，再检查一遍细节。'
  },
  review: {
    label: '可能不完整',
    tone: 'bg-warning-100 text-warning-700',
    hint: '有些选项或结局可能漏掉了，载入后对照原文补全。'
  },
  rejected: {
    label: '有文字对不上',
    tone: 'bg-danger-100 text-danger-700',
    hint: '图里有些文字不在你粘贴的内容里，载入后请逐条核对。'
  },
  no_chart: {
    label: '没有找到选项图',
    tone: 'bg-default-100 text-default-700',
    hint: '这段文字里没有可以整理成选项图的内容。'
  }
}

const generate = async () => {
  if (!text.value.trim() || loading.value) return
  loading.value = true
  reply.value = null
  try {
    reply.value = await api<DraftReply>('/v2/me/walkthrough-drafts', {
      method: 'POST',
      body: { text: text.value },
      timeout: 120_000
    })
  } catch (error) {
    useKunMessage(problemMessage(error, '生成失败'), 'error')
  } finally {
    loading.value = false
  }
}

const isBlank = computed(
  () =>
    doc.chart.value.segments.length <= 1 &&
    doc.chart.value.segments.every((s) => s.steps.every((st) => !st.text))
)

const load = async () => {
  const chart = reply.value?.chart
  if (!chart) return
  if (!isBlank.value) {
    const ok = await useKunAlert({
      title: '替换当前攻略图',
      message: '草稿会替换编辑器里现在的攻略图。这一步可以用 Ctrl/⌘ + Z 撤销。',
      type: 'warning',
      confirmText: '替换',
      cancelText: '取消'
    })
    if (!ok) return
  }
  const next = normalizeChart(chart)
  doc.edit((d) => Object.assign(d, next))
  if (doc.lang.value === 'zh-Hans' && KANA.test(JSON.stringify(next))) {
    doc.setLang('ja')
    useKunMessage('草稿是日文，已把攻略语言改为日文原文', 'info')
  }
  open.value = false
  useKunMessage('已载入草稿，记得对照原文检查一遍', 'success')
}
</script>

<template>
  <KunModal
    v-model="open"
    title="从文字生成攻略草稿"
    description="把你写好的文字攻略粘贴进来，模型会整理成选项图，载入后可以继续修改。原文只用来生成草稿，不会被保存。"
    size="lg"
    :is-dismissable="!loading"
  >
    <div class="space-y-4">
      <KunInfo color="warning" variant="flat" icon="lucide:copyright">
        只粘贴你自己写的内容。其他攻略站的说明、心得属于原作者，不要搬进来。
      </KunInfo>
      <KunTextarea
        v-model="text"
        :maxlength="MAX_CHARS"
        :rows="12"
        placeholder="例如：&#10;【START】&#10;回家&#10;和寧々说话&#10;◆存档1&#10;帮忙&#10;寧々END"
        :disabled="loading"
      />

      <div
        v-if="reply"
        class="border-default-200 space-y-2 rounded-lg border p-3 text-sm"
      >
        <div class="flex flex-wrap items-center gap-2">
          <span
            :class="
              cn(
                'rounded px-1.5 text-xs leading-5',
                VERDICT[reply.verdict].tone
              )
            "
          >
            {{ VERDICT[reply.verdict].label }}
          </span>
          <span v-if="reply.chart" class="text-default-500">
            {{ reply.chart.segments.length }} 条路线段 ·
            {{ reply.chart.endings.length }} 个结局 · 覆盖
            {{ Math.round(reply.coverage * 100) }}%
          </span>
          <span class="flex-1" />
          <span class="text-default-400 text-xs"
            >今天还能生成 {{ reply.remaining }} 次</span
          >
        </div>
        <p class="text-default-600">{{ VERDICT[reply.verdict].hint }}</p>
        <p v-if="reply.missed.length" class="text-default-500 text-xs">
          可能漏掉：{{ reply.missed.join('、') }}
        </p>
        <p v-if="reply.not_verbatim.length" class="text-danger-600 text-xs">
          对不上的文字：{{ reply.not_verbatim.join('、') }}
        </p>
      </div>

      <div class="flex items-center justify-end gap-2">
        <span v-if="loading" class="text-default-500 mr-auto text-sm">
          正在生成，通常需要 10～60 秒…
        </span>
        <KunButton variant="light" :disabled="loading" @click="open = false"
          >取消</KunButton
        >
        <KunButton
          v-if="reply?.chart"
          color="primary"
          variant="flat"
          @click="load"
        >
          用这个草稿替换当前攻略图
        </KunButton>
        <KunButton
          :color="reply?.chart ? 'default' : 'primary'"
          :variant="reply?.chart ? 'light' : 'solid'"
          :loading="loading"
          :disabled="!text.trim() || doc.readonly.value"
          @click="generate"
        >
          {{ reply ? '重新生成' : '生成草稿' }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
