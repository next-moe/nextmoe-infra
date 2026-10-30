<script setup lang="ts">
import {
  AGENT_API,
  AGENT_TOOL_AUTH_META,
  AGENT_TOOL_AUTH_OPTIONS,
  AGENT_TOOL_DEFAULTS,
  AGENT_TOOL_NAME_PATTERN,
  AGENT_TOOL_TIMEOUT_RANGE
} from '~/constants/agent'
import type {
  AgentSiteTool,
  AgentSiteToolForm,
  AgentSiteToolInput
} from '~~/shared/types/agent'
import type { Site } from '~~/shared/types/site'

const props = defineProps<{
  tool: AgentSiteTool | null
  sites: Site[]
  defaultSiteId: number
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [AgentSiteTool] }>()

const api = useApi('agent')

const form = ref<AgentSiteToolForm>({ ...AGENT_TOOL_DEFAULTS, allowed: [] })
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  const t = props.tool
  form.value = t
    ? {
        site_id: t.site_id,
        name: t.name,
        url: t.url,
        auth: t.auth,
        card_prefix: t.card_prefix,
        allowed: [...(t.allowed ?? [])],
        timeout_ms: t.timeout_ms,
        enabled: t.enabled
      }
    : {
        ...AGENT_TOOL_DEFAULTS,
        allowed: [],
        site_id: props.defaultSiteId || props.sites[0]?.id || 0
      }
  error.value = ''
})

const siteOptions = computed(() =>
  props.sites.map((s) => ({ value: s.id, label: `${s.name}（${s.domain}）` }))
)

const validate = () => {
  const f = form.value
  if (!f.site_id) return '请选择站点'
  if (!AGENT_TOOL_NAME_PATTERN.test(f.name))
    return '名称须以小写字母开头,只含小写字母、数字和下划线,最长 32 位'
  if (!/^https?:\/\/\S+$/.test(f.url.trim())) return 'MCP 地址须是 http(s) 链接'
  if (!AGENT_TOOL_NAME_PATTERN.test(f.card_prefix))
    return '卡片前缀须以小写字母开头,只含小写字母、数字和下划线,最长 32 位'
  const { min, max } = AGENT_TOOL_TIMEOUT_RANGE
  if (f.timeout_ms < min || f.timeout_ms > max)
    return `超时须在 ${min} 到 ${max} 毫秒之间`
  return ''
}

const save = async () => {
  error.value = validate()
  if (error.value) return
  const f = form.value
  const common: AgentSiteToolInput = {
    site_id: f.site_id,
    name: f.name,
    url: f.url.trim(),
    auth: f.auth,
    card_prefix: f.card_prefix,
    timeout_ms: f.timeout_ms,
    enabled: f.enabled
  }
  saving.value = true
  try {
    const res = props.tool
      ? await api.patch<AgentSiteTool>(
          `${AGENT_API}/site-tools/${props.tool.id}`,
          { ...common }
        )
      : await api.post<AgentSiteTool>(`${AGENT_API}/site-tools`, {
          ...common,
          allowed: []
        })
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.tool ? '已保存' : '已登记', 'success')
    open.value = false
    emit('saved', res.data)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="tool ? `编辑站点工具「${tool.name}」` : '登记站点工具'"
    size="lg"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunSelect
          v-if="siteOptions.length"
          v-model="form.site_id"
          label="站点"
          :options="siteOptions"
          searchable
        />
        <KunNumberInput
          v-else
          :model-value="form.site_id"
          label="站点 ID"
          :min="1"
          description="站点列表没有加载出来;填「站点管理」里的站点 ID"
          @update:model-value="form.site_id = $event ?? 0"
        />
        <KunInput
          v-model="form.name"
          label="名称"
          placeholder="kungal"
          :description="`模型看到的工具名是 ${form.name || '<名称>'}__<工具名>`"
          required
        />
      </div>
      <KunInput
        v-model="form.url"
        label="MCP 地址"
        placeholder="https://www.example.com/mcp"
        description="站点的 MCP 端点(Streamable HTTP),须能从 agent 服务访问到"
        required
      />
      <div class="grid gap-3 sm:grid-cols-2">
        <KunSelect
          v-model="form.auth"
          label="鉴权"
          :options="AGENT_TOOL_AUTH_OPTIONS"
          :description="AGENT_TOOL_AUTH_META[form.auth].hint"
        />
        <KunInput
          v-model="form.card_prefix"
          label="卡片前缀"
          placeholder="kungal"
          :description="`须与站点的卡片类型一致:这个服务器返回的卡片 kind 必须是 ${form.card_prefix || '<前缀>'}:<类型>,其它的会被丢弃`"
          required
        />
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <KunNumberInput
          :model-value="form.timeout_ms"
          label="超时 (ms)"
          :min="AGENT_TOOL_TIMEOUT_RANGE.min"
          :max="AGENT_TOOL_TIMEOUT_RANGE.max"
          :step="1000"
          description="单次工具调用的上限,1000–120000"
          @update:model-value="form.timeout_ms = $event ?? 0"
        />
        <KunSwitch v-model="form.enabled" label="启用" class-name="sm:mt-7" />
      </div>
      <p v-if="!tool" class="text-default-400 text-xs">
        登记后点「测试」列出服务器的工具,再勾选允许 AI
        助手调用的那些;不勾选时允许全部。
      </p>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
