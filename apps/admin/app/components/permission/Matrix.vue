<script setup lang="ts">
import type {
  PermissionDomainView,
  PermissionKeyRow,
  PermissionCell
} from '~~/shared/types/permission'
import { roleLabel } from '~/constants/roles'
import {
  IMMUTABLE_ROLE_NOTES,
  CELL_OP_LABELS,
  cellOp
} from '~/constants/permission'

defineProps<{ domain: PermissionDomainView; roles: string[] }>()
const emit = defineEmits<{ toggle: [row: PermissionKeyRow, role: string] }>()

const cellOf = (row: PermissionKeyRow, role: string): PermissionCell =>
  row.grants[role] ?? {
    granted: false,
    source: 'none',
    editable: false,
    can_deny: false,
    can_restore: false
  }

const isActionable = (row: PermissionKeyRow, role: string) =>
  cellOp(cellOf(row, role)) !== null

const cellHint = (row: PermissionKeyRow, role: string) => {
  const cell = cellOf(row, role)
  const op = cellOp(cell)
  if (op) return `点击${CELL_OP_LABELS[op]}`
  return cell.reason || IMMUTABLE_ROLE_NOTES[role] || '不可编辑'
}
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-baseline gap-2">
      <h2 class="text-foreground text-lg font-bold">{{ domain.title_zh }}</h2>
      <span class="text-default-400 font-mono text-sm">{{ domain.name }}</span>
    </div>

    <div class="overflow-x-auto">
      <table class="w-full min-w-[52rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">权限键</th>
            <th
              v-for="role in roles"
              :key="role"
              class="px-3 py-2 text-center font-medium"
              :class="IMMUTABLE_ROLE_NOTES[role] ? 'text-default-300' : ''"
            >
              {{ roleLabel(role) }}
              <span class="text-default-300 block font-mono text-xs">
                {{ role }}
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in domain.keys"
            :key="row.key"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">
              <div class="flex items-center gap-2">
                <span class="text-foreground font-mono break-all">
                  {{ row.key }}
                </span>
                <KunTooltip
                  v-if="row.non_delegable"
                  text="不可委派：只能通过改代码捆并部署来变更"
                  position="top"
                >
                  <KunIcon
                    name="lucide:lock"
                    class="text-default-400 size-4 shrink-0"
                  />
                </KunTooltip>
              </div>
              <p class="text-default-400 mt-0.5">{{ row.desc_zh }}</p>
            </td>

            <td
              v-for="role in roles"
              :key="role"
              class="px-3 py-2 text-center"
              :class="IMMUTABLE_ROLE_NOTES[role] ? 'bg-default-50' : ''"
            >
              <KunTooltip :text="cellHint(row, role)" position="top">
                <KunButton
                  v-if="isActionable(row, role)"
                  color="default"
                  variant="light"
                  size="sm"
                  is-icon-only
                  :aria-label="`${role} ${row.key}`"
                  @click="emit('toggle', row, role)"
                >
                  <KunIcon
                    v-if="cellOf(row, role).source === 'none'"
                    name="lucide:plus"
                    class="text-default-400 size-4"
                  />
                  <PermissionCellState v-else :cell="cellOf(row, role)" />
                </KunButton>

                <PermissionCellState v-else :cell="cellOf(row, role)" />
              </KunTooltip>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </KunCard>
</template>
