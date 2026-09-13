<script setup lang="ts">
import type {
  PermissionMatrix,
  PermissionAuditEntry,
  PermissionKeyRow
} from '~~/shared/types/permission'
import { AUDIT_LIST_LIMIT, cellOp } from '~/constants/permission'

const api = useApi()

const {
  data: matrixData,
  status,
  refresh: refreshMatrix,
  error
} = await useApiFetch<PermissionMatrix>('/admin/permissions/matrix')

const {
  data: auditData,
  refresh: refreshAudit,
  error: auditError
} = await useApiFetch<PermissionAuditEntry[]>('/admin/permissions/audit', {
  query: { limit: AUDIT_LIST_LIMIT }
})

const matrix = computed(() => matrixData.value)
const auditEntries = computed(() => auditData.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const confirmOpen = ref(false)
const pendingRow = ref<PermissionKeyRow | null>(null)
const pendingRole = ref('')
const submitting = ref(false)

const askToggle = (row: PermissionKeyRow, role: string) => {
  const cell = row.grants[role]
  if (!cell || !cellOp(cell)) return
  pendingRow.value = row
  pendingRole.value = role
  confirmOpen.value = true
}

const SUCCESS_MESSAGES = {
  grant: '已授予',
  revoke: '已撤销叠加授权',
  deny: '已撤销该权限',
  restore: '已恢复'
} as const

const confirmToggle = async () => {
  const row = pendingRow.value
  const role = pendingRole.value
  const cell = row && role ? row.grants[role] : null
  const op = cell ? cellOp(cell) : null
  if (!row || !role || !op) return

  submitting.value = true
  try {
    const response =
      op === 'grant' || op === 'deny'
        ? await api.post('/admin/permissions/overrides', {
            role,
            permission: row.key,
            effect: op
          })
        : await api.delete('/admin/permissions/overrides', {
            role,
            permission: row.key
          })

    if (response.code === 0) {
      useKunMessage(SUCCESS_MESSAGES[op], 'success')
      confirmOpen.value = false
      await Promise.all([refreshMatrix(), refreshAudit()])
    } else {
      useKunMessage(response.message || '操作失败', 'error')
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-foreground text-2xl font-bold">权限矩阵</h1>
      <p class="text-default-500 mt-1">
        点击可操作的格子即修改叠加层：可以给 creator/moderator/admin
        加上代码捆没有的权限，也可以收回代码捆给它们的权限。ren
        行是锁死后的恢复保险，只读。
      </p>
    </div>

    <CommonFetchError v-if="error" @retry="refreshMatrix" />

    <div v-else-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <template v-else-if="matrix">
      <PermissionLegend :manages-permissions="matrix.manages_permissions" />

      <PermissionMatrix
        v-for="domain in matrix.domains"
        :key="domain.name"
        :domain="domain"
        :roles="matrix.roles"
        @toggle="askToggle"
      />

      <PermissionAuditList
        :entries="auditEntries"
        :has-error="Boolean(auditError)"
        @retry="refreshAudit"
      />

      <PermissionConfirmModal
        v-model:open="confirmOpen"
        :row="pendingRow"
        :role="pendingRole"
        :submitting="submitting"
        @confirm="confirmToggle"
      />
    </template>
  </div>
</template>
