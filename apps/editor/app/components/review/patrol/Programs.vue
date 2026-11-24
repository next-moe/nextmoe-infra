<script setup lang="ts">
import type { CanonProgram } from '#shared/types/canon'
import { CANON_PROGRAM_MODES } from '~/constants/canon'
import { canonMessage } from '~/utils/canon/record'
import { formatRelative } from '~/utils/format'

const api = useEditorApi()
const programs = ref<CanonProgram[]>([])
const note = ref('')
const busy = ref(false)

const load = async () => {
  try {
    programs.value = (
      await api<{ items: CanonProgram[] }>('/v3/catalog/programs')
    ).items
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  }
}

const setMode = async (program: CanonProgram, mode: string) => {
  if (mode === program.mode) {
    return
  }
  busy.value = true
  try {
    await api(`/v3/catalog/programs/${program.key}/mode`, {
      method: 'PUT',
      body: { mode, note: note.value.trim() || '编辑台设置' }
    })
    useKunMessage('已设置', 'success')
    note.value = ''
    await load()
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  } finally {
    busy.value = false
  }
}

const modeOptions = Object.entries(CANON_PROGRAM_MODES).map(([value, m]) => ({
  value,
  label: m.label
}))

onMounted(load)
</script>

<template>
  <div class="space-y-3">
    <KunCard v-for="p in programs" :key="p.key" bordered>
      <div class="space-y-3">
        <div class="flex items-center gap-2">
          <span class="text-foreground font-medium">{{ p.key }}</span>
          <KunChip size="sm" variant="flat">{{ p.medium }}</KunChip>
        </div>
        <p class="text-default-500 text-sm">{{ p.doc }}</p>
        <dl class="text-default-600 grid grid-cols-2 gap-2 text-sm sm:grid-cols-5">
          <div>写入 {{ p.commits }} 次</div>
          <div>未巡查 {{ p.unpatrolled }}</div>
          <div>被人撤回 {{ p.undone }}</div>
          <div>待处理 {{ p.todos.open ?? 0 }}</div>
          <div>已驳回 {{ p.todos.rejected ?? 0 }}</div>
        </dl>
        <div class="flex flex-wrap items-center gap-3">
          <KunRadioGroup
            :model-value="p.mode"
            :options="modeOptions"
            :disabled="busy"
            variant="pill"
            orientation="horizontal"
            size="sm"
            aria-label="运行模式"
            @update:model-value="(v: string | number) => setMode(p, String(v))"
          />
          <KunInput
            v-model="note"
            size="sm"
            placeholder="改模式的原因"
            aria-label="改模式的原因"
          />
        </div>
        <p class="text-default-400 text-xs">
          {{ CANON_PROGRAM_MODES[p.mode]?.description }}
          <template v-if="p.set_by">
            {{ p.set_by }} 设置{{ p.set_at ? '于 ' + formatRelative(p.set_at) : '' }}
            {{ p.note }}
          </template>
        </p>
      </div>
    </KunCard>
  </div>
</template>
