<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { AGENT_API } from '~/constants/agent'
import type { AgentSiteTool, AgentSiteToolProbe } from '~~/shared/types/agent'

const props = defineProps<{ tool: AgentSiteTool; probe: AgentSiteToolProbe }>()
const emit = defineEmits<{ saved: [AgentSiteTool]; close: [] }>()

const api = useApi('agent')

const listed = computed(() => props.probe.tools ?? [])
const allowAll = ref(false)
const checked = ref<string[]>([])
const saving = ref(false)
const error = ref('')

watch(
  () => [props.tool, props.probe] as const,
  () => {
    const allowed = props.tool.allowed ?? []
    allowAll.value = !allowed.length
    checked.value = listed.value
      .filter((t) => !allowed.length || allowed.includes(t.name))
      .map((t) => t.name)
    error.value = ''
  },
  { immediate: true }
)

const stale = computed(() =>
  (props.tool.allowed ?? []).filter(
    (a) => !listed.value.some((t) => t.name === a)
  )
)

const toggle = (name: string, on: boolean) => {
  checked.value = on
    ? [...checked.value, name]
    : checked.value.filter((n) => n !== name)
}

const save = async () => {
  error.value = ''
  if (!allowAll.value && !checked.value.length) {
    error.value = '至少勾选一个工具,或打开「允许全部」'
    return
  }
  saving.value = true
  try {
    const res = await api.patch<AgentSiteTool>(
      `${AGENT_API}/site-tools/${props.tool.id}`,
      { allowed: allowAll.value ? [] : checked.value }
    )
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage('已更新允许的工具', 'success')
    emit('saved', res.data)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div
    :class="
      cn(
        'space-y-3 rounded-xl border p-4',
        probe.ok
          ? 'border-default-200 bg-content1'
          : 'border-danger-200 bg-danger-50'
      )
    "
  >
    <div class="flex items-start justify-between gap-3">
      <p
        :class="
          cn(
            'flex items-center gap-2 text-sm font-medium',
            probe.ok ? 'text-success-700' : 'text-danger-700'
          )
        "
      >
        <KunIcon
          :name="probe.ok ? 'lucide:circle-check' : 'lucide:circle-x'"
          class="size-4 shrink-0"
        />
        「{{ tool.name }}」{{
          probe.ok ? `连接成功,列出 ${listed.length} 个工具` : '连接失败'
        }}
      </p>
      <KunButton
        size="sm"
        variant="light"
        is-icon-only
        aria-label="收起测试结果"
        @click="emit('close')"
      >
        <KunIcon name="lucide:x" class="size-4" />
      </KunButton>
    </div>

    <p v-if="!probe.ok" class="text-danger-700 font-mono text-xs break-all">
      {{ probe.error }}
    </p>

    <template v-else>
      <KunSwitch
        v-model="allowAll"
        label="允许全部"
        description="服务器列出的工具都可调用,包括以后新增的"
      />
      <div v-if="!allowAll" class="grid gap-2 sm:grid-cols-2">
        <KunCheckBox
          v-for="t in listed"
          :key="t.name"
          :model-value="checked.includes(t.name)"
          :label="t.model_name"
          :description="t.description"
          color="primary"
          class-name="rounded-lg border border-default-200 px-3 py-2"
          @update:model-value="toggle(t.name, !!$event)"
        />
      </div>
      <div v-else class="flex flex-wrap gap-1">
        <KunChip
          v-for="t in listed"
          :key="t.name"
          color="primary"
          variant="flat"
          size="xs"
        >
          {{ t.model_name }}
        </KunChip>
      </div>
      <p v-if="stale.length" class="text-warning-600 text-xs">
        已允许但服务器没有列出:{{ stale.join('、') }};保存后会移除。
      </p>
      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end">
        <KunButton color="primary" size="sm" :loading="saving" @click="save">
          保存允许的工具
        </KunButton>
      </div>
    </template>
  </div>
</template>
