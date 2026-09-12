<script setup lang="ts">
import type { SettingsKeyView } from '~~/shared/types/settings'
import { formatSettingValue } from '~/constants/settings'

const props = defineProps<{
  row: SettingsKeyView | null
  submitting: boolean
  scopeKind: 'platform' | 'site'
}>()
const emit = defineEmits<{ confirm: [note: string] }>()

const open = defineModel<boolean>('open', { required: true })

const note = ref('')

watch([open, () => props.row], ([isOpen]) => {
  if (!isOpen) return
  note.value = ''
})
</script>

<template>
  <KunModal v-model="open" aria-label="撤销覆盖">
    <div class="space-y-4">
      <h2 class="text-foreground text-xl font-bold">撤销覆盖</h2>

      <div v-if="row" class="space-y-2 text-sm">
        <p v-if="scopeKind === 'site'" class="text-default-500">
          将删除站点覆盖值,该键回退到平台生效值
          <span class="text-foreground font-mono">
            {{ formatSettingValue(row.kind, row.inherited) }}
          </span>
        </p>
        <p v-else class="text-default-500">
          将删除
          <span class="text-foreground font-mono font-semibold break-all">
            {{ row.key }}
          </span>
          的覆盖值，该键回退到环境变量地板(若所属服务设置了)或代码默认
          <span class="text-foreground font-mono">
            {{ formatSettingValue(row.kind, row.default) }}
          </span>
          。
        </p>
        <KunTextarea
          v-model="note"
          label="备注(可选,最多 512 字)"
          :rows="3"
          :maxlength="512"
        />
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton
          color="warning"
          :loading="submitting"
          @click="emit('confirm', note)"
        >
          撤销覆盖
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
