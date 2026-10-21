<script setup lang="ts">
import type { KunUIColor } from '@kungal/ui-core'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { mergeTiming } from '~/utils/creation'

const props = defineProps<{ title: string }>()
const emit = defineEmits<{ 'open-result': []; 'open-submit': [] }>()

const workbench = useSplitWorkbench()
const { request, preview, busy, dirty, draft } = workbench

const STATE_COLORS: Record<string, KunUIColor> = {
  open: 'warning',
  approved: 'primary',
  executed: 'success',
  rejected: 'danger',
  withdrawn: 'default',
  undone: 'secondary'
}
const STATE_LABELS: Record<string, string> = {
  ...MERGE_STATE_LABELS,
  executed: '已拆分'
}

const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  ticker = setInterval(() => (now.value = Date.now()), 30_000)
})
onBeforeUnmount(() => clearInterval(ticker))

const timing = computed(() =>
  request.value
    ? mergeTiming(request.value.state, request.value.execute_after, now.value)
    : null
)
const canUndo = computed(
  () =>
    workbench.isReviewer.value &&
    request.value?.state === 'executed' &&
    !!request.value.undo_until &&
    new Date(request.value.undo_until).getTime() > now.value
)
const back = computed(() =>
  !request.value
    ? workbench.workId.value
      ? { to: `/works/${workbench.workId.value}`, label: '返回作品' }
      : { to: '/', label: '返回工作台' }
    : workbench.isReviewer.value
      ? { to: '/review/requests?tab=split', label: '返回请求列表' }
      : { to: '/me/split-requests', label: '我的拆分请求' }
)

const decision = ref<'reject' | 'undo' | null>(null)

const withdraw = async () => {
  const ok = await useKunAlert({
    title: '撤回拆分请求',
    message: '撤回后这条请求关闭，需要时可以重新发起。',
    type: 'warning',
    confirmText: '撤回',
    cancelText: '取消'
  })
  if (ok && (await workbench.withdraw())) {
    useKunMessage('拆分请求已撤回', 'success')
  }
}
const approve = async () => {
  if (await workbench.decide('approve')) {
    useKunMessage('已批准，冷静期结束后执行', 'success')
  }
}
const execute = async () => {
  const ok = await useKunAlert({
    title: '立即执行拆分',
    message:
      '执行后会新建作品并移走所选发行。执行后的一段时间内仍可以撤销，撤销会把新作品并回原作品。',
    type: 'warning',
    confirmText: '执行',
    cancelText: '取消'
  })
  if (ok && (await workbench.decide('execute'))) {
    useKunMessage('拆分已执行', 'success')
  }
}
</script>

<template>
  <header
    class="border-default-200 bg-content1 flex h-12 shrink-0 items-center gap-2 border-b px-2 md:px-3"
  >
    <NuxtLink
      :to="back.to"
      class="text-default-600 hover:bg-default-100 hover:text-foreground flex h-8 shrink-0 items-center gap-1.5 rounded-lg px-2 text-sm transition-colors"
    >
      <KunIcon name="lucide:arrow-left" class="size-4" />
      <span class="hidden xl:inline">{{ back.label }}</span>
    </NuxtLink>
    <KunIcon
      name="lucide:chevron-right"
      class="text-default-300 hidden size-4 shrink-0 sm:block"
    />

    <div class="flex min-w-0 flex-1 items-center gap-2">
      <span
        class="text-default-500 hidden shrink-0 items-center gap-1 text-sm md:inline-flex"
      >
        <KunIcon name="lucide:git-fork" class="size-4" />
        {{ props.title }}
      </span>
      <span
        v-if="preview || request"
        class="flex min-w-0 items-center gap-1.5 text-sm font-semibold"
      >
        <span class="truncate">
          {{ preview?.target_name ?? request?.target_name }}
        </span>
        <KunIcon name="lucide:split" class="text-default-400 size-4 shrink-0" />
        <span class="truncate">
          {{ request?.new_work_name || draft.displayName || '新作品' }}
        </span>
      </span>
      <KunChip
        v-if="request"
        size="sm"
        variant="flat"
        :color="STATE_COLORS[request.state] ?? 'default'"
        class-name="shrink-0"
      >
        {{ STATE_LABELS[request.state] ?? request.state }}
      </KunChip>
    </div>

    <div class="flex shrink-0 items-center gap-1 md:gap-2">
      <KunButton
        v-if="preview"
        variant="flat"
        size="sm"
        @click="emit('open-result')"
      >
        <KunIcon name="lucide:file-diff" class="size-4" />
        <span class="hidden sm:inline">查看结果</span>
      </KunButton>

      <KunButton
        v-if="workbench.mode === 'compose'"
        color="primary"
        size="sm"
        :disabled="!preview || workbench.blocked.value"
        @click="emit('open-submit')"
      >
        <KunIcon name="lucide:send" class="size-4" />
        提交拆分请求
      </KunButton>

      <template v-else-if="request">
        <template v-if="workbench.isReviewer.value">
          <template v-if="dirty">
            <KunButton
              variant="light"
              size="sm"
              @click="workbench.resetAmendment"
            >
              还原
            </KunButton>
            <KunButton
              variant="bordered"
              size="sm"
              :disabled="workbench.blocked.value"
              :loading="busy === 'amend'"
              @click="workbench.saveAmendment"
            >
              保存修改
            </KunButton>
          </template>
          <KunButton
            v-if="request.state === 'open' || request.state === 'approved'"
            variant="light"
            color="danger"
            size="sm"
            @click="decision = 'reject'"
          >
            驳回
          </KunButton>
          <KunButton
            v-if="request.state === 'open'"
            color="primary"
            size="sm"
            :disabled="workbench.blocked.value"
            :loading="busy === 'approve' || busy === 'amend'"
            @click="approve"
          >
            <KunIcon name="lucide:check" class="size-4" />
            {{ dirty ? '保存并批准' : '批准' }}
          </KunButton>
          <KunButton
            v-else-if="request.state === 'approved'"
            color="warning"
            size="sm"
            :disabled="!timing?.canExecute || dirty || workbench.blocked.value"
            :loading="busy === 'execute'"
            @click="execute"
          >
            <KunIcon name="lucide:play" class="size-4" />
            {{
              timing?.canExecute
                ? '立即执行'
                : `冷静期还剩 ${formatWait(timing?.waitMs ?? 0)}`
            }}
          </KunButton>
          <KunButton
            v-else-if="canUndo"
            variant="bordered"
            color="danger"
            size="sm"
            @click="decision = 'undo'"
          >
            <KunIcon name="lucide:undo-2" class="size-4" />
            撤销拆分
          </KunButton>
        </template>
        <KunButton
          v-if="workbench.isRequester.value && request.state === 'open'"
          variant="light"
          size="sm"
          :loading="busy === 'withdraw'"
          @click="withdraw"
        >
          撤回请求
        </KunButton>
      </template>

      <LayoutAccountMenu />
    </div>

    <SplitWorkbenchDecisionModal :kind="decision" @close="decision = null" />
  </header>
</template>
