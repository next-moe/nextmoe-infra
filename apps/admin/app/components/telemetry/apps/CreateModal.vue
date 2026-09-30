<script setup lang="ts">
import { TELEMETRY_SERVICE_NAME_RE } from '~/constants/telemetry'
import type { TelemetryApp } from '~~/shared/types/telemetry'

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi('telemetry')
const serviceName = ref('')
const displayName = ref('')
const saving = ref(false)
const error = ref('')

watch(open, (v) => {
  if (!v) return
  serviceName.value = ''
  displayName.value = ''
  error.value = ''
})

const save = async () => {
  error.value = ''
  const name = serviceName.value.trim()
  const display = displayName.value.trim()
  if (!TELEMETRY_SERVICE_NAME_RE.test(name)) {
    error.value = '服务名须匹配 ^[a-z][a-z0-9-]{1,62}$（小写字母开头，2–63 位）'
    return
  }
  if (!display) {
    error.value = '显示名不能为空'
    return
  }
  saving.value = true
  try {
    const res = await api.post<TelemetryApp>('/admin/telemetry/apps', {
      service_name: name,
      display_name: display
    })
    if (res.code !== 0) {
      error.value = res.message || '创建失败'
      useKunMessage(res.message || '创建失败', 'error')
      return
    }
    useKunMessage('已创建', 'success')
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" title="新建应用" size="md">
    <div class="space-y-4">
      <KunInput
        v-model="serviceName"
        label="服务名"
        placeholder="kungal"
        description="小写字母开头，仅字母、数字、连字符，2–63 位"
        required
      />
      <KunInput
        v-model="displayName"
        label="显示名"
        placeholder="KUN Galgame"
        required
      />
      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <div class="flex justify-end gap-3">
        <KunButton
          color="default"
          variant="flat"
          :disabled="saving"
          @click="open = false"
        >
          取消
        </KunButton>
        <KunButton color="primary" :loading="saving" @click="save">
          创建
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
