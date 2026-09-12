<script setup lang="ts">
import type {
  TrustSubjectKind,
  TrustCreateSubjectKindRequest,
  TrustPatchSubjectKindRequest,
  TrustEnsureSubjectKindItem,
  TrustBatchSubjectKindsRequest,
  TrustEnsureSubjectKindsResponse,
  TrustEnsureSubjectKindResult
} from '~~/shared/types/trust'

const api = useApi('trust')

const site = ref('')
const { data, refresh, error } = await useApiFetch<TrustSubjectKind[]>(
  '/admin/trust/subject-kinds',
  { query: computed(() => ({ site: site.value || undefined })) },
  'trust'
)
const kinds = computed<TrustSubjectKind[]>(() => data.value ?? [])

const createOpen = ref(false)
const form = reactive({
  site: '',
  key: '',
  callback_url: '',
  callback_secret: '',
  notify_on_dismiss: false
})
const creating = ref(false)

const openCreate = () => {
  form.site = site.value || ''
  form.key = ''
  form.callback_url = ''
  form.callback_secret = ''
  form.notify_on_dismiss = false
  createOpen.value = true
}

const create = async () => {
  if (!form.site.trim() || !form.key.trim()) {
    useKunMessage('站点与 key 必填', 'warn')
    return
  }
  creating.value = true
  try {
    const body: TrustCreateSubjectKindRequest = {
      site: form.site.trim(),
      key: form.key.trim(),
      notify_on_dismiss: form.notify_on_dismiss
    }
    if (form.callback_url.trim()) body.callback_url = form.callback_url.trim()
    if (form.callback_secret.trim())
      body.callback_secret = form.callback_secret.trim()
    const res = await api.post('/admin/trust/subject-kinds', body)
    if (res.code === 0) {
      useKunMessage('已注册', 'success')
      createOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '注册失败', 'error')
    }
  } finally {
    creating.value = false
  }
}

const editOpen = ref(false)
const editTarget = ref<TrustSubjectKind | null>(null)
const editForm = reactive({
  callback_url: '',
  callback_secret: '',
  notify_on_dismiss: false
})
const saving = ref(false)

const openEdit = (k: TrustSubjectKind) => {
  editTarget.value = k
  editForm.callback_url = k.callback_url ?? ''
  editForm.callback_secret = ''
  editForm.notify_on_dismiss = k.notify_on_dismiss ?? false
  editOpen.value = true
}

const patch = async (
  k: TrustSubjectKind,
  body: TrustPatchSubjectKindRequest
) => {
  const res = await api.patch(`/admin/trust/subject-kinds/${k.id}`, body)
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
    const body: TrustPatchSubjectKindRequest = {
      callback_url: editForm.callback_url.trim(),
      notify_on_dismiss: editForm.notify_on_dismiss
    }
    if (editForm.callback_secret.trim())
      body.callback_secret = editForm.callback_secret.trim()
    if (await patch(editTarget.value, body)) {
      useKunMessage('已更新回调配置', 'success')
      editOpen.value = false
    }
  } finally {
    saving.value = false
  }
}

const toggleDeprecated = async (k: TrustSubjectKind) => {
  if (await patch(k, { is_deprecated: !k.is_deprecated })) {
    useKunMessage(k.is_deprecated ? '已启用' : '已弃用', 'success')
  }
}

const batchOpen = ref(false)
const batchForm = reactive({
  site: '',
  keysText: '',
  callback_url: '',
  callback_secret: '',
  notify_on_dismiss: false
})
const batching = ref(false)

const openBatch = () => {
  batchForm.site = site.value || ''
  batchForm.keysText = ''
  batchForm.callback_url = ''
  batchForm.callback_secret = ''
  batchForm.notify_on_dismiss = false
  batchOpen.value = true
}

const summarizeBatch = (results: TrustEnsureSubjectKindResult[]): string => {
  const counts = { created: 0, updated: 0, unchanged: 0, deprecated_skipped: 0 }
  for (const r of results) counts[r.result] += 1
  return `已收敛:新建 ${counts.created} · 更新 ${counts.updated} · 未变 ${counts.unchanged} · 跳过(已弃用) ${counts.deprecated_skipped}`
}

const runBatch = async () => {
  const batchSite = batchForm.site.trim()
  const keys = batchForm.keysText
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
  if (!batchSite || !keys.length) {
    useKunMessage('站点与至少一个 key 必填', 'warn')
    return
  }
  if (keys.length > 50) {
    useKunMessage('单次最多 50 个 key', 'warn')
    return
  }
  const cbURL = batchForm.callback_url.trim()
  const cbSecret = batchForm.callback_secret.trim()
  const kinds: TrustEnsureSubjectKindItem[] = keys.map((key) => {
    const item: TrustEnsureSubjectKindItem = { key }
    if (cbURL) item.callback_url = cbURL
    if (cbSecret) item.callback_secret = cbSecret
    if (batchForm.notify_on_dismiss) item.notify_on_dismiss = true
    return item
  })
  const body: TrustBatchSubjectKindsRequest = { site: batchSite, kinds }
  batching.value = true
  try {
    const res = await api.post<TrustEnsureSubjectKindsResponse>(
      '/admin/trust/subject-kinds/batch',
      body
    )
    if (res.code === 0) {
      useKunMessage(summarizeBatch(res.data.results ?? []), 'success')
      batchOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '批量添加失败', 'error')
    }
  } finally {
    batching.value = false
  }
}
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-center gap-2">
      <h2 class="text-foreground text-lg font-bold">主体类型</h2>
      <KunInput
        v-model="site"
        placeholder="按站点过滤"
        class="w-40"
      />
      <div class="ml-auto flex gap-2">
        <KunButton
          color="default"
          variant="flat"
          size="sm"
          @click="openBatch"
        >
          <KunIcon name="lucide:list-plus" class="mr-1 size-4" />
          批量添加
        </KunButton>
        <KunButton color="primary" size="sm" @click="openCreate">
          <KunIcon name="lucide:plus" class="mr-1 size-4" />
          新建
        </KunButton>
      </div>
    </div>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="overflow-x-auto">
      <table class="w-full min-w-[36rem] text-sm">
        <thead class="text-default-500">
          <tr>
            <th class="px-2 py-2 text-left font-medium">站点</th>
            <th class="px-2 py-2 text-left font-medium">key</th>
            <th class="px-2 py-2 text-left font-medium">回调</th>
            <th class="px-2 py-2 text-left font-medium">状态</th>
            <th class="px-2 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="k in kinds"
            :key="k.id"
            class="border-default-200 border-t align-top"
          >
            <td class="px-2 py-2">{{ k.site }}</td>
            <td class="px-2 py-2 font-mono">{{ k.key }}</td>
            <td class="px-2 py-2">
              <div class="flex flex-wrap items-center gap-1">
                <span v-if="k.callback_url" class="text-default-400 max-w-[14rem] truncate">
                  {{ k.callback_url }}
                </span>
                <span v-else class="text-default-300">—</span>
                <KunChip v-if="k.has_secret" color="success" variant="flat" size="xs">
                  密钥
                </KunChip>
                <KunChip v-if="k.notify_on_dismiss" color="info" variant="flat" size="xs">
                  驳回回调
                </KunChip>
              </div>
            </td>
            <td class="px-2 py-2">
              <KunChip
                :color="k.is_deprecated ? 'default' : 'success'"
                variant="flat"
                size="xs"
              >
                {{ k.is_deprecated ? '已弃用' : '启用中' }}
              </KunChip>
            </td>
            <td class="px-2 py-2 text-right">
              <div class="flex justify-end gap-2">
                <KunButton
                  color="default"
                  variant="flat"
                  size="sm"
                  @click="openEdit(k)"
                >
                  编辑
                </KunButton>
                <KunButton
                  :color="k.is_deprecated ? 'success' : 'warning'"
                  variant="flat"
                  size="sm"
                  @click="toggleDeprecated(k)"
                >
                  {{ k.is_deprecated ? '启用' : '弃用' }}
                </KunButton>
              </div>
            </td>
          </tr>
          <tr v-if="!kinds.length && !error">
            <td colspan="5" class="text-default-400 px-2 py-8 text-center">
              暂无主体类型
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <KunModal v-model="createOpen" aria-label="注册主体类型">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">注册主体类型</h2>
        <KunInput v-model="form.site" label="站点" placeholder="如 kungal" />
        <KunInput v-model="form.key" label="key" placeholder="如 topic / reply" />
        <KunInput
          v-model="form.callback_url"
          label="回调 URL(可选)"
          placeholder="https://…"
        />
        <KunInput
          v-model="form.callback_secret"
          label="回调密钥(可选)"
          placeholder="HMAC 密钥"
        />
        <KunSwitch
          v-model="form.notify_on_dismiss"
          label="驳回时回调(释放持留/自动隐藏)"
        />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="createOpen = false">
            取消
          </KunButton>
          <KunButton color="primary" :loading="creating" @click="create">
            注册
          </KunButton>
        </div>
      </div>
    </KunModal>

    <KunModal v-model="batchOpen" aria-label="批量添加主体类型">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">批量添加主体类型</h2>
        <p class="text-default-500 text-sm">
          一行一个 key,共享下方的回调 / 驳回配置,后端逐 key
          收敛:不存在则新建、已存在则按提供字段更新、相同则不动;<span
            class="text-foreground font-semibold"
            >已弃用的 key 不会被复活</span
          >。
        </p>
        <KunInput v-model="batchForm.site" label="站点" placeholder="如 kungal" />
        <KunTextarea
          v-model="batchForm.keysText"
          :rows="6"
          label="keys(每行一个,单次最多 50)"
          placeholder="forum_topic&#10;forum_reply&#10;resource_post"
        />
        <KunInput
          v-model="batchForm.callback_url"
          label="共享回调 URL(可选)"
          placeholder="https://…"
        />
        <KunInput
          v-model="batchForm.callback_secret"
          label="共享回调密钥(可选)"
          placeholder="HMAC 密钥"
        />
        <KunSwitch
          v-model="batchForm.notify_on_dismiss"
          label="驳回时回调(释放持留/自动隐藏)"
        />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="batchOpen = false">
            取消
          </KunButton>
          <KunButton color="primary" :loading="batching" @click="runBatch">
            提交
          </KunButton>
        </div>
      </div>
    </KunModal>

    <KunModal v-model="editOpen" aria-label="编辑回调">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">
          编辑回调 · {{ editTarget?.site }}/{{ editTarget?.key }}
        </h2>
        <KunInput
          v-model="editForm.callback_url"
          label="回调 URL"
          placeholder="留空则清除"
        />
        <KunInput
          v-model="editForm.callback_secret"
          label="回调密钥"
          placeholder="留空则保持不变"
        />
        <KunSwitch
          v-model="editForm.notify_on_dismiss"
          label="驳回时回调(释放持留/自动隐藏)"
        />
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
