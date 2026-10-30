<script setup lang="ts">
import {
  LLM_API,
  LLM_IMPORT_ACTION_META,
  LLM_IMPORT_KIND_LABEL
} from '~/constants/llm'
import type {
  LlmGatewayConfig,
  LlmImportKind,
  LlmImportResult
} from '~~/shared/types/llm'

const emit = defineEmits<{ applied: [] }>()

const api = useApi('ai')

const exporting = ref(false)
const file = ref<File | null>(null)
const config = ref<LlmGatewayConfig | null>(null)
const preview = ref<LlmImportResult | null>(null)
const previewing = ref(false)
const applying = ref(false)
const error = ref('')

const exportConfig = async () => {
  exporting.value = true
  try {
    const res = await api.get<LlmGatewayConfig>(`${LLM_API}/config`)
    if (res.code !== 0) {
      useKunMessage(res.message || '导出失败', 'error')
      return
    }
    const blob = new Blob([JSON.stringify(res.data, null, 2)], {
      type: 'application/json'
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `llm-gateway-config-${llmCstDays(1)[0]}.json`
    a.click()
    URL.revokeObjectURL(url)
    useKunMessage('已导出', 'success')
  } finally {
    exporting.value = false
  }
}

const parseConfig = (text: string): LlmGatewayConfig | null => {
  try {
    const parsed: unknown = JSON.parse(text)
    if (!parsed || typeof parsed !== 'object') return null
    const body =
      'data' in parsed && parsed.data && typeof parsed.data === 'object'
        ? parsed.data
        : parsed
    if (!('version' in body)) return null
    const { $schema: _schema, ...rest } = body as LlmGatewayConfig & {
      $schema?: string
    }
    return rest
  } catch {
    return null
  }
}

const runImport = (apply: boolean) =>
  api.post<LlmImportResult>(
    `${LLM_API}/config/import${apply ? '?apply=true' : ''}`,
    { ...config.value }
  )

watch(file, async (f) => {
  preview.value = null
  config.value = null
  error.value = ''
  if (!f) return
  const parsed = parseConfig(await f.text())
  if (!parsed) {
    error.value = '这不是网关导出的配置文件(JSON 解析失败或缺少 version)'
    return
  }
  config.value = parsed
  previewing.value = true
  try {
    const res = await runImport(false)
    if (res.code !== 0) {
      error.value = res.message || '预览失败'
      return
    }
    preview.value = res.data
  } finally {
    previewing.value = false
  }
})

const groups = computed(() => {
  const changes = preview.value?.changes ?? []
  return (Object.keys(LLM_IMPORT_KIND_LABEL) as LlmImportKind[])
    .map((kind) => {
      const list = changes.filter((c) => c.kind === kind)
      return {
        kind,
        changed: list.filter((c) => c.action !== 'unchanged'),
        unchanged: list.filter((c) => c.action === 'unchanged')
      }
    })
    .filter((g) => g.changed.length || g.unchanged.length)
})

const pending = computed(
  () =>
    preview.value?.changes.filter((c) => c.action !== 'unchanged').length ?? 0
)

const apply = async () => {
  const confirmed = await useKunAlert({
    title: `应用导入的 ${pending.value} 项改动?`,
    message: '只会新增或更新,不会删除目标环境里已有的其它配置。',
    type: 'warning',
    confirmText: '应用'
  })
  if (!confirmed) return
  applying.value = true
  try {
    const res = await runImport(true)
    if (res.code !== 0) {
      error.value = res.message || '导入失败'
      return
    }
    useKunMessage(`已导入 ${pending.value} 项改动`, 'success')
    file.value = null
    emit('applied')
  } finally {
    applying.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <KunCard class-name="space-y-3 p-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p class="text-foreground font-medium">导出配置</p>
          <p class="text-default-500 text-sm">
            供应商、模型、路由、场景和访问密钥的设置,存成一个 JSON
            文件;不含供应商 API key,也不含访问密钥的 secret。
          </p>
        </div>
        <KunButton color="primary" :loading="exporting" @click="exportConfig">
          <KunIcon name="lucide:download" class="mr-2 size-4" />
          导出
        </KunButton>
      </div>
    </KunCard>

    <KunCard class-name="space-y-4 p-4">
      <div>
        <p class="text-foreground font-medium">导入配置</p>
        <p class="text-default-500 text-sm">
          按名称合并:只新增或更新,从不删除,所以目标环境多出来的配置不受影响。选好文件后先预览,确认无误再应用。导入的供应商需要重新填
          API key,新建的访问密钥需要重新签发 secret。
        </p>
      </div>
      <KunFileInput
        v-model="file"
        accept=".json,application/json"
        hint="选择导出的 JSON 文件"
        @error-pick="error = $event"
      />

      <div v-if="previewing" class="text-default-500 text-sm">预览中…</div>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>

      <template v-if="preview">
        <div
          v-if="preview.warnings.length"
          class="border-warning-200 bg-warning-50 space-y-1 rounded-xl border p-4"
        >
          <p
            class="text-warning-700 flex items-center gap-2 text-sm font-medium"
          >
            <KunIcon name="lucide:triangle-alert" class="size-4" />
            {{ preview.warnings.length }} 条警告
          </p>
          <p
            v-for="(w, i) in preview.warnings"
            :key="i"
            class="text-warning-700 text-sm break-all"
          >
            {{ w }}
          </p>
        </div>

        <p class="text-default-500 text-sm">
          共 {{ preview.changes.length }} 项,其中
          <span class="text-foreground font-medium">{{ pending }}</span>
          项会改变。
        </p>

        <div v-for="g in groups" :key="g.kind" class="space-y-2">
          <p class="text-foreground text-sm font-medium">
            {{ LLM_IMPORT_KIND_LABEL[g.kind] }}
          </p>
          <div
            v-for="c in g.changed"
            :key="`${c.kind}-${c.name}`"
            class="border-default-200 flex items-center gap-2 rounded-lg border px-3 py-1.5"
          >
            <KunChip
              :color="LLM_IMPORT_ACTION_META[c.action].color"
              variant="flat"
              size="xs"
            >
              {{ LLM_IMPORT_ACTION_META[c.action].label }}
            </KunChip>
            <span class="text-foreground font-mono text-sm break-all">
              {{ c.name }}
            </span>
          </div>
          <KunAccordion v-if="g.unchanged.length" variant="bordered">
            <KunAccordionItem
              :value="`${g.kind}-unchanged`"
              :title="`不变 ${g.unchanged.length} 项`"
            >
              <div class="flex flex-wrap gap-1">
                <KunChip
                  v-for="c in g.unchanged"
                  :key="`${c.kind}-${c.name}`"
                  color="default"
                  variant="flat"
                  size="xs"
                >
                  {{ c.name }}
                </KunChip>
              </div>
            </KunAccordionItem>
          </KunAccordion>
        </div>

        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="file = null">
            取消
          </KunButton>
          <KunButton
            color="primary"
            :disabled="!pending"
            :loading="applying"
            @click="apply"
          >
            {{ pending ? `应用 ${pending} 项改动` : '没有需要应用的改动' }}
          </KunButton>
        </div>
      </template>
    </KunCard>
  </div>
</template>
