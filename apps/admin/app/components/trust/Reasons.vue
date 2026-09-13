<script setup lang="ts">
import type {
  TrustReason,
  TrustCreateReasonRequest,
  TrustPatchReasonRequest
} from '~~/shared/types/trust'

const api = useApi('trust')

const site = ref('')
const { data, refresh, error } = await useApiFetch<TrustReason[]>(
  '/admin/trust/report-reasons',
  { query: computed(() => ({ site: site.value || undefined })) },
  'trust'
)
const reasons = computed<TrustReason[]>(() => data.value ?? [])

const createOpen = ref(false)
const form = reactive({ key: '', name_cn: '', severity: 1, site: '' })
const creating = ref(false)

const openCreate = () => {
  form.key = ''
  form.name_cn = ''
  form.severity = 1
  form.site = site.value || ''
  createOpen.value = true
}

const create = async () => {
  if (!form.key.trim() || !form.name_cn.trim()) {
    useKunMessage('key 与名称必填', 'warn')
    return
  }
  creating.value = true
  try {
    const body: TrustCreateReasonRequest = {
      key: form.key.trim(),
      name_cn: form.name_cn.trim(),
      severity: form.severity ?? 1
    }
    if (form.site.trim()) body.site = form.site.trim()
    const res = await api.post('/admin/trust/report-reasons', body)
    if (res.code === 0) {
      useKunMessage('已创建', 'success')
      createOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '创建失败', 'error')
    }
  } finally {
    creating.value = false
  }
}

const editOpen = ref(false)
const editTarget = ref<TrustReason | null>(null)
const editForm = reactive({ name_cn: '', severity: 1 })
const saving = ref(false)

const openEdit = (r: TrustReason) => {
  editTarget.value = r
  editForm.name_cn = r.name_cn
  editForm.severity = r.severity
  editOpen.value = true
}

const patch = async (r: TrustReason, body: TrustPatchReasonRequest) => {
  const res = await api.patch(`/admin/trust/report-reasons/${r.id}`, body)
  if (res.code === 0) {
    await refresh()
    return true
  }
  useKunMessage(res.message || '更新失败', 'error')
  return false
}

const saveEdit = async () => {
  if (!editTarget.value) return
  saving.value = true
  try {
    const ok = await patch(editTarget.value, {
      name_cn: editForm.name_cn.trim(),
      severity: editForm.severity ?? 1
    })
    if (ok) {
      useKunMessage('已更新', 'success')
      editOpen.value = false
    }
  } finally {
    saving.value = false
  }
}

const toggleDeprecated = async (r: TrustReason) => {
  if (await patch(r, { is_deprecated: !r.is_deprecated })) {
    useKunMessage(r.is_deprecated ? '已启用' : '已弃用', 'success')
  }
}
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-center gap-2">
      <h2 class="text-foreground text-lg font-bold">举报理由</h2>
      <KunInput v-model="site" placeholder="按站点过滤" class="w-40" />
      <KunButton color="primary" size="sm" class="ml-auto" @click="openCreate">
        <KunIcon name="lucide:plus" class="mr-1 size-4" />
        新建
      </KunButton>
    </div>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="overflow-x-auto">
      <table class="w-full min-w-[36rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-2 py-2 text-left font-medium">key</th>
            <th class="px-2 py-2 text-left font-medium">名称</th>
            <th class="px-2 py-2 text-right font-medium">严重度</th>
            <th class="px-2 py-2 text-left font-medium">归属</th>
            <th class="px-2 py-2 text-left font-medium">状态</th>
            <th class="px-2 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="r in reasons"
            :key="r.id"
            class="border-default-200 border-t align-top"
          >
            <td class="px-2 py-2 font-mono">{{ r.key }}</td>
            <td class="px-2 py-2">{{ r.name_cn }}</td>
            <td class="px-2 py-2 text-right">{{ r.severity }}</td>
            <td class="px-2 py-2">
              <KunChip
                :color="r.site ? 'secondary' : 'info'"
                variant="flat"
                size="xs"
              >
                {{ r.site || '全局' }}
              </KunChip>
            </td>
            <td class="px-2 py-2">
              <KunChip
                :color="r.is_deprecated ? 'default' : 'success'"
                variant="flat"
                size="xs"
              >
                {{ r.is_deprecated ? '已弃用' : '启用中' }}
              </KunChip>
            </td>
            <td class="px-2 py-2 text-right">
              <div class="flex justify-end gap-2">
                <KunButton
                  color="default"
                  variant="flat"
                  size="sm"
                  @click="openEdit(r)"
                >
                  编辑
                </KunButton>
                <KunButton
                  :color="r.is_deprecated ? 'success' : 'warning'"
                  variant="flat"
                  size="sm"
                  @click="toggleDeprecated(r)"
                >
                  {{ r.is_deprecated ? '启用' : '弃用' }}
                </KunButton>
              </div>
            </td>
          </tr>
          <tr v-if="!reasons.length && !error">
            <td colspan="6" class="text-default-400 px-2 py-8 text-center">
              暂无理由
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <KunModal v-model="createOpen" aria-label="新建举报理由">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">新建举报理由</h2>
        <KunInput v-model="form.key" label="key" placeholder="如 abuse / spam" />
        <KunInput v-model="form.name_cn" label="名称" placeholder="如 辱骂骚扰" />
        <KunNumberInput v-model="form.severity" label="严重度" :min="0" />
        <KunInput
          v-model="form.site"
          label="站点(可选,留空为全局)"
          placeholder="如 kungal"
        />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="createOpen = false">
            取消
          </KunButton>
          <KunButton color="primary" :loading="creating" @click="create">
            创建
          </KunButton>
        </div>
      </div>
    </KunModal>

    <KunModal v-model="editOpen" aria-label="编辑举报理由">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">
          编辑理由 · {{ editTarget?.key }}
        </h2>
        <KunInput v-model="editForm.name_cn" label="名称" />
        <KunNumberInput v-model="editForm.severity" label="严重度" :min="0" />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="editOpen = false">
            取消
          </KunButton>
          <KunButton color="primary" :loading="saving" @click="saveEdit">
            保存
          </KunButton>
        </div>
      </div>
    </KunModal>
  </KunCard>
</template>
