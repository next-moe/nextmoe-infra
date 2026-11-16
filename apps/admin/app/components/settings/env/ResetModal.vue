<script setup lang="ts">
import type { EnvVarView } from '~~/shared/types/settings'
import { envScopeLabel } from '~/constants/settings-env'

const props = defineProps<{
  row: EnvVarView | null
  scope: string
  submitting: boolean
}>()
const emit = defineEmits<{ confirm: [note: string] }>()

const open = defineModel<boolean>('open', { required: true })
const note = ref('')

watch(open, (isOpen) => {
  if (isOpen) note.value = ''
})

const fallback = computed(() =>
  props.scope !== '' && props.row?.stored.some((s) => s.scope === '')
    ? '「全部服务」的存储值'
    : '环境变量或代码默认值'
)
</script>

<template>
  <KunModal v-model="open" size="md" aria-label="撤销存储值">
    <div v-if="row" class="space-y-4">
      <div>
        <h2 class="text-foreground text-xl font-bold">撤销存储值</h2>
        <p class="text-foreground mt-1 font-mono break-all">{{ row.key }}</p>
        <p class="text-default-500 mt-1">
          作用域 {{ envScopeLabel(scope) }};撤销后服务下次启动回落到{{
            fallback
          }}。
        </p>
      </div>

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
        <KunButton
          color="warning"
          :loading="submitting"
          @click="emit('confirm', note)"
        >
          撤销
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
