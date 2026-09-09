<script setup lang="ts">
import type {
  PermissionKeyRow,
  PermissionCell
} from '~~/shared/types/permission'
import { roleLabel } from '~/constants/roles'
import {
  BLAST_RADIUS_NOTE,
  CELL_OP_LABELS,
  cellOp,
  type PermissionCellOp,
  type PermissionChipColor
} from '~/constants/permission'

const props = defineProps<{
  row: PermissionKeyRow | null
  role: string
  submitting: boolean
}>()
const emit = defineEmits<{ confirm: [] }>()

const open = defineModel<boolean>('open', { required: true })

const cell = computed<PermissionCell | null>(
  () => props.row?.grants[props.role] ?? null
)
const op = computed<PermissionCellOp | null>(() =>
  cell.value ? cellOp(cell.value) : null
)
const actionLabel = computed(() =>
  op.value ? CELL_OP_LABELS[op.value] : '变更'
)

const OUTCOMES: Record<PermissionCellOp, string> = {
  grant: '该角色的每一位持有者立即获得此能力。',
  revoke: '删除这条叠加授权后，该角色回到代码捆（地板），不会低于地板。',
  deny: '该角色的每一位持有者立即失去此能力，直到有人恢复它——这会让该角色低于代码捆。',
  restore: '删除这条撤销记录后，该角色回到代码捆（地板），重新获得此能力。'
}

const CONFIRM_COLORS: Record<PermissionCellOp, PermissionChipColor> = {
  grant: 'primary',
  revoke: 'warning',
  deny: 'danger',
  restore: 'primary'
}

const outcome = computed(() => (op.value ? OUTCOMES[op.value] : ''))
const confirmColor = computed(() =>
  op.value ? CONFIRM_COLORS[op.value] : 'primary'
)
</script>

<template>
  <KunModal v-model="open" :aria-label="actionLabel">
    <div class="space-y-4">
      <h2 class="text-foreground text-xl font-bold">{{ actionLabel }}</h2>

      <div v-if="row" class="space-y-2 text-sm">
        <p class="text-default-500">
          将对角色
          <span class="text-foreground font-semibold">
            {{ roleLabel(role) }}（{{ role }}）
          </span>
          执行「{{ actionLabel }}」：
          <span class="text-foreground font-mono font-semibold break-all">
            {{ row.key }}
          </span>
        </p>
        <p class="text-default-400">{{ row.desc_zh }}</p>

        <KunInfo
          v-if="op === 'deny'"
          color="danger"
          variant="flat"
          icon="lucide:shield-alert"
          :description="outcome"
        />
        <p v-else class="text-default-500">{{ outcome }}</p>

        <p class="text-default-500">
          {{ BLAST_RADIUS_NOTE }}；此变更会记入审计。
        </p>
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton
          :color="confirmColor"
          :loading="submitting"
          @click="emit('confirm')"
        >
          确认{{ actionLabel }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
