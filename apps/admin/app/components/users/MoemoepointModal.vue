<script setup lang="ts">
import {
  moemoepointReasonLabel,
  moemoepointSourceLabel
} from '~/constants/moemoepoint'

interface MoemoepointLogRow {
  id: number
  delta: number
  reason: string
  source_app: string
  source_name?: string
  ref: string
  actor_user_id: number
  note: string
  created_at: string
}

interface Props {
  open: boolean
  user: { uuid: string; name: string; moemoepoint: number } | null
}
const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:open', v: boolean): void
  (e: 'success'): void
}>()

const api = useApi()

const balance = ref(0)
const amount = ref('') // bound to KunInput (string); parsed on submit
const note = ref('')
const submitting = ref(false)
const errorMsg = ref('')

const logs = ref<MoemoepointLogRow[]>([])
const logLoading = ref(false)

const loadLog = async () => {
  if (!props.user) return
  logLoading.value = true
  try {
    const res = await api.get<{ items: MoemoepointLogRow[]; has_more: boolean }>(
      `/admin/users/${props.user.uuid}/moemoepoint/log`,
      { limit: 20 }
    )
    if (res.code === 0) logs.value = res.data.items ?? []
  } finally {
    logLoading.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open && props.user) {
      balance.value = props.user.moemoepoint
      amount.value = ''
      note.value = ''
      errorMsg.value = ''
      loadLog()
    }
  }
)

const close = () => emit('update:open', false)

const adjust = async (sign: 1 | -1) => {
  if (!props.user || submitting.value) return
  const amt = Number(amount.value)
  if (!Number.isFinite(amt) || amt <= 0) {
    errorMsg.value = '请输入正整数数量'
    return
  }
  submitting.value = true
  errorMsg.value = ''
  try {
    const res = await api.post<{ balance: number; applied: boolean }>(
      `/admin/users/${props.user.uuid}/moemoepoint`,
      {
        delta: sign * Math.floor(amt),
        note: note.value,
        idempotency_key: crypto.randomUUID()
      }
    )
    if (res.code === 0) {
      balance.value = res.data.balance
      amount.value = ''
      note.value = ''
      emit('success')
      await loadLog()
    } else {
      errorMsg.value = res.message || '操作失败'
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    :model-value="open"
    size="lg"
    @update:model-value="close"
    aria-label="萌萌点管理"
  >
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-foreground text-xl font-bold">萌萌点管理</h2>
        <span class="text-default-500 text-sm">{{ user?.name }}</span>
      </div>

      <div class="bg-content2 flex items-baseline gap-2 rounded-lg p-4">
        <span class="text-default-500 text-sm">当前余额</span>
        <span class="text-primary text-2xl font-bold">{{ balance }}</span>
      </div>

      <div class="space-y-2">
        <KunInput
          v-model="amount"
          type="number"
          label="数量"
          placeholder="正整数"
          autocomplete="off"
        />
        <KunInput
          v-model="note"
          label="备注（可选）"
          placeholder="例如：活动奖励 / 违规处罚"
          autocomplete="off"
        />
        <p v-if="errorMsg" class="text-danger text-sm">{{ errorMsg }}</p>
        <div class="flex gap-3">
          <KunButton
            color="success"
            :disabled="submitting"
            class-name="flex-1"
            @click="adjust(1)"
          >
            <KunIcon name="lucide:plus" class="mr-1 size-4" />
            发放
          </KunButton>
          <KunButton
            color="danger"
            :disabled="submitting"
            class-name="flex-1"
            @click="adjust(-1)"
          >
            <KunIcon name="lucide:minus" class="mr-1 size-4" />
            扣除
          </KunButton>
        </div>
      </div>

      <div>
        <h3 class="text-default-500 mb-2 text-sm font-medium">最近变动</h3>
        <div v-if="logLoading" class="text-default-400 py-4 text-center text-sm">
          加载中…
        </div>
        <div
          v-else-if="logs.length === 0"
          class="text-default-400 py-4 text-center text-sm"
        >
          暂无记录
        </div>
        <div v-else class="max-h-64 space-y-1 overflow-y-auto">
          <div
            v-for="row in logs"
            :key="row.id"
            class="border-default-200 flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm"
          >
            <div class="min-w-0">
              <span
                :class="row.delta >= 0 ? 'text-success' : 'text-danger'"
                class="font-mono font-semibold"
              >
                {{ row.delta >= 0 ? '+' : '' }}{{ row.delta }}
              </span>
              <span class="text-default-500 ml-2">{{ moemoepointReasonLabel(row.reason, row.ref) }}</span>
              <span class="text-default-300 ml-1 text-xs">来自 {{ row.source_name || moemoepointSourceLabel(row.source_app) }}</span>
              <p v-if="row.note" class="text-default-400 truncate text-xs">
                {{ row.note }}
              </p>
            </div>
            <span class="text-default-400 shrink-0 text-xs">
              {{ new Date(row.created_at).toLocaleString() }}
            </span>
          </div>
        </div>
      </div>

      <div class="flex justify-end">
        <KunButton variant="flat" color="default" @click="close">关闭</KunButton>
      </div>
    </div>
  </KunModal>
</template>
