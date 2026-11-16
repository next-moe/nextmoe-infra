<script setup lang="ts">
import type { EnvVarView } from '~~/shared/types/settings'
import { envScopeLabel } from '~/constants/settings-env'

const props = defineProps<{
  row: EnvVarView | null
  scopes: string[]
  submitting: boolean
}>()
const emit = defineEmits<{
  save: [scope: string, value: string, note: string]
}>()

const open = defineModel<boolean>('open', { required: true })

const scope = ref('')
const value = ref('')
const note = ref('')

const scopeOptions = computed(() =>
  props.scopes.map((s) => ({ value: s, label: envScopeLabel(s) }))
)

const envHolders = computed(() =>
  (props.row?.services ?? [])
    .filter(
      (v) =>
        v.source === 'env' && (scope.value === '' || v.service === scope.value)
    )
    .map((v) => v.service)
)

const prefill = () => {
  const row = props.row
  if (!row || row.secret) return ''
  const stored = row.stored.find((s) => s.scope === scope.value)
  if (stored?.value) return stored.value
  const own = row.services.find((v) => v.service === scope.value)
  return own?.value ?? (row.diverged ? '' : (row.services[0]?.value ?? ''))
}

watch([open, () => props.row], ([isOpen, row]) => {
  if (!isOpen || !row) return
  scope.value = row.stored[0]?.scope ?? ''
  note.value = ''
  value.value = prefill()
})

watch(scope, () => {
  if (open.value) value.value = prefill()
})

const submit = () => {
  if (value.value === '') {
    useKunMessage('值不能为空;要删除请撤销', 'warn')
    return
  }
  emit('save', scope.value, value.value, note.value)
}
</script>

<template>
  <KunModal v-model="open" size="lg" :aria-label="row?.key ?? '写入变量'">
    <div v-if="row" class="space-y-4">
      <div>
        <h2 class="text-foreground font-mono text-xl font-bold break-all">
          {{ row.key }}
        </h2>
        <p class="text-default-500 mt-1">
          写入配置中心;对应服务下次启动时读取。
          <template v-if="row.secret">密钥写入后不能再查看明文。</template>
        </p>
      </div>

      <KunSelect
        v-model="scope"
        label="作用域"
        :options="scopeOptions"
        description="「全部服务」对每个服务生效;单个服务的值优先于它"
      />

      <KunInput
        v-model="value"
        :type="row.secret ? 'password' : 'text'"
        label="值"
        autocomplete="off"
      />

      <p v-if="envHolders.length" class="text-warning-600 text-sm">
        {{ envHolders.join('、') }} 的环境变量里仍有这个值,环境变量优先:
        写入后请从 Dokploy 删掉它,否则这里的值不会生效。
      </p>

      <KunTextarea
        v-model="note"
        label="备注(可选,最多 512 字)"
        :rows="3"
        :maxlength="512"
      />

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :loading="submitting" @click="submit">
          写入
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
