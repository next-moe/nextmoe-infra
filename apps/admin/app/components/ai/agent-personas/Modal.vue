<script setup lang="ts">
import {
  AGENT_API,
  AGENT_DEFAULT_SITE,
  AGENT_PERSONA_PLACEHOLDER,
  AGENT_PERSONA_PROMPT_HINT
} from '~/constants/agent'
import type {
  AgentPersona,
  AgentPersonaInput,
  AgentSuggestionDraft,
  AgentSuggestionGroup
} from '~~/shared/types/agent'
import type { Site } from '~~/shared/types/site'

const props = defineProps<{
  persona: AgentPersona | null
  sites: Site[]
  builtin: AgentSuggestionGroup[]
  defaultSiteId: number
}>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('agent')

const siteId = ref(AGENT_DEFAULT_SITE)
const name = ref('')
const avatar = ref('')
const prompt = ref('')
const greetings = ref('')
const groups = ref<AgentSuggestionDraft[]>([])
const active = ref(true)
const saving = ref(false)
const error = ref('')

const lines = (text: string) =>
  text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)

watch(open, (v) => {
  if (!v) return
  const p = props.persona
  siteId.value = p?.site_id ?? props.defaultSiteId
  name.value = p?.name ?? ''
  avatar.value = p?.avatar ?? ''
  prompt.value = p?.prompt ?? ''
  greetings.value = (p?.greetings ?? []).join('\n')
  groups.value = (p?.suggestions ?? []).map((g) => ({
    label: g.label,
    icon: g.icon,
    web_search: g.web_search,
    promptsText: (g.prompts ?? []).join('\n')
  }))
  active.value = p?.active ?? true
  error.value = ''
})

const siteOptions = computed(() => [
  { value: AGENT_DEFAULT_SITE, label: '默认（所有没有自己人设的站点）' },
  ...props.sites.map((s) => ({
    value: s.id,
    label: `${s.name}（${s.domain}）`
  }))
])

const avatarOk = computed(
  () => !avatar.value.trim() || avatar.value.trim().startsWith('https://')
)

const save = async () => {
  error.value = ''
  if (!name.value.trim() || !prompt.value.trim()) {
    error.value = '名字和人设是必填的'
    return
  }
  if (!avatarOk.value) {
    error.value = '头像须为 https 图片地址'
    return
  }
  const suggestions = groups.value.map((g) => ({
    label: g.label.trim(),
    icon: g.icon.trim() || 'sparkles',
    web_search: g.web_search,
    prompts: lines(g.promptsText)
  }))
  if (
    suggestions.some(
      (g) => !g.label || !g.prompts.length || g.prompts.length > 12
    )
  ) {
    error.value = '每个建议分组都要有按钮文字和 1–12 条问题'
    return
  }
  const body: AgentPersonaInput = {
    site_id: siteId.value,
    name: name.value.trim(),
    avatar: avatar.value.trim(),
    prompt: prompt.value.trim(),
    greetings: lines(greetings.value),
    suggestions,
    active: active.value
  }
  saving.value = true
  try {
    const res = props.persona
      ? await api.patch<AgentPersona>(
          `${AGENT_API}/personas/${props.persona.id}`,
          body
        )
      : await api.post<AgentPersona>(`${AGENT_API}/personas`, body)
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(
      props.persona ? '已保存，下一问立即生效' : '已创建',
      'success'
    )
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="persona ? `编辑人设「${persona.name}」` : '新建人设'"
    size="xl"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <KunSelect v-model="siteId" label="站点" :options="siteOptions" />
        <KunInput v-model="name" label="名字" placeholder="小萌" required />
      </div>
      <div class="flex items-end gap-3">
        <KunAvatar
          :user="{ id: 0, name: name || '?', avatar: avatarOk ? avatar : '' }"
          size="lg"
          :is-navigation="false"
        />
        <KunInput
          v-model="avatar"
          label="头像"
          placeholder="https://…"
          description="https 图片地址；留空用站点前端自带的头像"
          class-name="flex-1"
        />
      </div>
      <KunTextarea
        v-model="prompt"
        label="人设"
        :placeholder="AGENT_PERSONA_PLACEHOLDER"
        :description="AGENT_PERSONA_PROMPT_HINT"
        :rows="6"
        :maxlength="8000"
      />
      <KunTextarea
        v-model="greetings"
        label="开场白（每行一条，随机显示一条）"
        :rows="3"
      />
      <AiAgentPersonasSuggestions v-model="groups" :builtin="builtin" />
      <KunSwitch
        v-model="active"
        label="启用"
        description="启用后该站点的其他人设会自动停用"
      />

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
