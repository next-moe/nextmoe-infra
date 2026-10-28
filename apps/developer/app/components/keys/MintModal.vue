<script setup lang="ts">
import { DEV_DEFAULT_SCOPES, DEV_MINTABLE_SCOPES } from '~/constants/dev'
import type { DevKeyMinted } from '~~/shared/types/dev'

const props = defineProps<{ clientId: string }>()
const emit = defineEmits<{ minted: [DevKeyMinted] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const name = ref('')
const test = ref(false)
const scopes = ref<string[]>([...DEV_DEFAULT_SCOPES])
const error = ref('')
const isLoading = ref(false)

watch(open, (val) => {
  if (!val) return
  name.value = ''
  test.value = false
  scopes.value = [...DEV_DEFAULT_SCOPES]
  error.value = ''
})

const toggleScope = (s: string) => {
  const i = scopes.value.indexOf(s)
  if (i >= 0) scopes.value.splice(i, 1)
  else scopes.value.push(s)
}

const handleSubmit = async () => {
  error.value = ''
  if (!name.value.trim()) {
    error.value = '请填写密钥名称'
    return
  }
  if (scopes.value.length === 0) {
    error.value = '请至少选择一个 scope'
    return
  }
  isLoading.value = true
  try {
    const body: Record<string, unknown> = {
      name: name.value.trim(),
      test: test.value,
      scopes: scopes.value
    }
    const res = await api.post<DevKeyMinted>(
      `/dev/apps/${props.clientId}/keys`,
      body
    )
    if (res.code === 0 && res.data) {
      open.value = false
      emit('minted', res.data)
    } else {
      error.value = res.message || '生成失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="md" aria-label="生成新密钥">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">生成新密钥</h2>

      <KunInput
        v-model="name"
        label="密钥名称"
        placeholder="例如：生产环境 / CI 流水线"
        required
      />

      <div>
        <span class="mb-1 block text-sm font-medium text-default-500">
          Scope（权限范围）
          <span class="text-xs text-default-400">— 默认勾选公开只读 scope</span>
        </span>
        <div class="flex flex-wrap gap-2">
          <KunCheckBox
            v-for="s in DEV_MINTABLE_SCOPES"
            :key="s"
            :model-value="scopes.includes(s)"
            :label="s"
            color="primary"
            class-name="rounded-lg border border-default-200 bg-content1 px-3 py-1.5 hover:border-primary"
            @update:model-value="toggleScope(s)"
          />
        </div>
        <p class="mt-1 text-xs text-default-400">
          — 每把密钥都会自动带上 letmoe:files（letmoe 下载直链），无需勾选。
        </p>
      </div>

      <div class="rounded-lg border border-default-200 p-3">
        <KunSwitch v-model="test" label="测试密钥（nmk_test_ 前缀）" />
        <p class="mt-1 text-xs text-default-400">
          — 测试密钥用于开发/联调；正式接入请使用生产密钥（nmk_live_）。
        </p>
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon
            v-if="isLoading"
            name="lucide:loader-circle"
            class="mr-2 size-4 animate-spin"
          />
          生成密钥
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
