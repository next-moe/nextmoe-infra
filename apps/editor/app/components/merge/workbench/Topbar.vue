<script setup lang="ts">
import type { KunUIColor } from '@kungal/ui-core'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { mergeTiming } from '~/utils/creation'

const props = defineProps<{ title: string }>()
const emit = defineEmits<{
  'open-nav': []
  'open-result': []
  'open-submit': []
}>()

const workbench = useMergeWorkbench()
const { request, preview, busy, dirty } = workbench

const STATE_COLORS: Record<string, KunUIColor> = {
  open: 'warning',
  approved: 'primary',
  executed: 'success',
  rejected: 'danger',
  withdrawn: 'default',
  undone: 'secondary'
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
const canSubmit = computed(
  () =>
    !!preview.value &&
    !workbench.blocked.value &&
    !preview.value.open_request_id
)

const rejectOpen = ref(false)
const undoOpen = ref(false)

const withdraw = async () => {
  const ok = await useKunAlert({
    title: '撤回合并请求',
    message: '撤回后这条请求关闭，需要时可以重新发起。',
    type: 'warning',
    confirmText: '撤回',
    cancelText: '取消'
  })
  if (ok && (await workbench.withdraw())) {
    useKunMessage('合并请求已撤回', 'success')
  }
}

const approve = async () => {
  if (await workbench.decide('approve')) {
    useKunMessage('已批准，冷静期结束后执行', 'success')
  }
}

const execute = async () => {
  const ok = await useKunAlert({
    title: '立即执行合并',
    message: '执行后被并入的条目改为重定向。执行后的一段时间内仍可以撤销。',
    type: 'warning',
    confirmText: '执行',
    cancelText: '取消'
  })
  if (ok && (await workbench.decide('execute'))) {
    useKunMessage('合并已执行', 'success')
  }
}
</script>

<template>
  <header
    class="border-default-200 bg-content1 flex h-12 shrink-0 items-center gap-2 border-b px-2 md:px-3"
  >
    <KunButton
      variant="light"
      size="sm"
      is-icon-only
      aria-label="打开分区导航"
      class-name="lg:hidden shrink-0"
      @click="emit('open-nav')"
    >
      <KunIcon name="lucide:menu" class="size-5" />
    </KunButton>
    <NuxtLink
      to="/"
      class="hidden shrink-0 items-center sm:flex"
      aria-label="未萌编辑台"
    >
      <img src="/favicon.webp" alt="" class="size-7 rounded-lg" />
    </NuxtLink>
    <KunIcon
      name="lucide:chevron-right"
      class="text-default-300 hidden size-4 shrink-0 sm:block"
    />

    <div class="flex min-w-0 flex-1 items-center gap-2">
      <span
        class="text-default-500 hidden shrink-0 items-center gap-1 text-sm md:inline-flex"
      >
        <KunIcon name="lucide:git-merge" class="size-4" />
        {{ props.title }}
      </span>
      <span
        v-if="preview"
        class="flex min-w-0 items-center gap-1.5 text-sm font-semibold"
      >
        <span class="truncate">{{ preview.source.name }}</span>
        <KunIcon
          name="lucide:arrow-right"
          class="text-default-400 size-4 shrink-0"
        />
        <span class="truncate">{{ preview.target.name }}</span>
      </span>
      <KunChip
        v-if="request"
        size="sm"
        variant="flat"
        :color="STATE_COLORS[request.state] ?? 'default'"
        class-name="shrink-0"
      >
        {{ MERGE_STATE_LABELS[request.state] ?? request.state }}
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

      <template v-if="workbench.mode === 'compose'">
        <KunButton
          color="primary"
          size="sm"
          :disabled="!canSubmit"
          @click="emit('open-submit')"
        >
          <KunIcon name="lucide:send" class="size-4" />
          提交合并请求
        </KunButton>
      </template>

      <template v-else-if="request">
        <template v-if="workbench.isReviewer.value">
          <template v-if="dirty">
            <KunButton
              variant="light"
              size="sm"
              @click="workbench.resetChoices"
            >
              还原
            </KunButton>
            <KunButton
              variant="bordered"
              size="sm"
              :loading="busy === 'amend'"
              @click="workbench.saveChoices"
            >
              保存修改
            </KunButton>
          </template>
          <KunButton
            v-if="request.state === 'open' || request.state === 'approved'"
            variant="light"
            color="danger"
            size="sm"
            @click="rejectOpen = true"
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
            :disabled="!timing?.canExecute || dirty"
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
            @click="undoOpen = true"
          >
            <KunIcon name="lucide:undo-2" class="size-4" />
            撤销合并
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

    <MergeWorkbenchRejectModal v-model="rejectOpen" />
    <MergeWorkbenchUndoModal v-model="undoOpen" />
  </header>
</template>
