<script setup lang="ts">
import type { KunDropdownItem } from '@kungal/ui-vue'
import type { WtIssue } from '#shared/types/walkthrough'

const props = defineProps<{
  entityId: string | null
  workId: string | null
  workTitle: string
  changeCount: number
  canSubmit: boolean
  submitHint: string
}>()

const emit = defineEmits<{
  submit: []
  changes: []
  discard: []
  shortcuts: []
  rail: []
}>()

const issuesOpen = defineModel<boolean>('issuesOpen', { default: false })
const pane = defineModel<'start' | 'end'>('pane', { default: 'start' })

const doc = useWalkthroughDocument()
const { link } = useReturnLink()

const issueCount = computed(() => doc.issues.value.length)

const issuesPopover = ref<{ open: () => void; close: () => void } | null>(null)
watch(issuesOpen, (requested) => {
  if (!requested) return
  issuesPopover.value?.open()
  issuesOpen.value = false
})

const moreItems = computed<KunDropdownItem[]>(() => [
  ...(props.workId
    ? [{ key: 'work', label: '打开所属作品', icon: 'lucide:gamepad-2' }]
    : []),
  ...(props.entityId
    ? [{ key: 'history', label: '修订历史', icon: 'lucide:history' }]
    : []),
  { key: 'shortcuts', label: '快捷键', icon: 'lucide:keyboard', shortcut: '?' },
  { type: 'separator' as const },
  {
    key: 'discard',
    label: props.entityId ? '放弃全部修改' : '清空草稿',
    icon: 'lucide:trash-2',
    color: 'danger' as const,
    disabled: !props.changeCount
  }
] as KunDropdownItem[])

const onMore = async (item: KunDropdownItem) => {
  if (item.key === 'work' && props.workId) {
    window.open(`/works/${props.workId}/edit?section=walkthroughs`, '_blank', 'noopener')
  } else if (item.key === 'history' && props.entityId) {
    window.open(`/history/walkthrough/${props.entityId}`, '_blank', 'noopener')
  } else if (item.key === 'shortcuts') {
    emit('shortcuts')
  } else if (item.key === 'discard') {
    const ok = await useKunAlert({
      title: props.entityId ? '放弃全部修改' : '清空草稿',
      message: '这会丢掉所有还没有提交的修改，不能撤销。',
      type: 'warning',
      confirmText: '放弃',
      cancelText: '保留'
    })
    if (ok) emit('discard')
  }
}

const jump = (issue: WtIssue) => {
  issuesPopover.value?.close()
  if (issue.target) doc.select(issue.target, true)
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
      aria-label="打开角色与结局"
      class-name="xl:hidden shrink-0"
      @click="emit('rail')"
    >
      <KunIcon name="lucide:panel-left" class="size-5" />
    </KunButton>
    <NuxtLink to="/" class="hidden shrink-0 items-center sm:flex" aria-label="未萌编辑台">
      <img src="/favicon.webp" alt="" class="size-7 rounded-lg" />
    </NuxtLink>
    <KunIcon name="lucide:chevron-right" class="text-default-300 hidden size-4 shrink-0 sm:block" />

    <div class="flex min-w-0 flex-1 items-center gap-2">
      <span class="text-default-500 hidden shrink-0 items-center gap-1 text-sm md:inline-flex">
        <KunIcon name="lucide:route" class="size-4" />
        {{ props.entityId ? '编辑攻略' : '新建攻略' }}
      </span>
      <span class="truncate text-sm font-semibold">{{ props.workTitle }}</span>
      <span
        v-if="doc.hidden.value"
        class="bg-warning-100 text-warning-700 shrink-0 rounded px-1.5 text-[11px] leading-5"
      >
        已隐藏
      </span>
    </div>

    <div class="flex shrink-0 items-center gap-1 md:gap-2">
      <KunButton
        v-if="link"
        :href="link.url"
        variant="light"
        size="sm"
        class-name="hidden 2xl:inline-flex"
      >
        <KunIcon name="lucide:arrow-left" class="size-4" />
        返回{{ link.label }}
      </KunButton>

      <div class="hidden items-center md:flex">
        <KunTooltip text="撤销" shortcut="Mod+Z" position="bottom">
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="撤销"
            :disabled="!doc.canUndo.value"
            @click="doc.undo"
          >
            <KunIcon name="lucide:undo-2" class="size-4" />
          </KunButton>
        </KunTooltip>
        <KunTooltip text="重做" shortcut="Mod+Shift+Z" position="bottom">
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="重做"
            :disabled="!doc.canRedo.value"
            @click="doc.redo"
          >
            <KunIcon name="lucide:redo-2" class="size-4" />
          </KunButton>
        </KunTooltip>
      </div>

      <KunButtonGroup class-name="lg:hidden">
        <KunButton
          size="sm"
          :variant="pane === 'start' ? 'flat' : 'light'"
          @click="pane = 'start'"
        >
          大纲
        </KunButton>
        <KunButton
          size="sm"
          :variant="pane === 'end' ? 'flat' : 'light'"
          @click="pane = 'end'"
        >
          图
        </KunButton>
      </KunButtonGroup>

      <KunPopover
        ref="issuesPopover"
        position="bottom-end"
        aria-label="需要修正的问题"
        inner-class="w-96"
      >
        <template #trigger>
          <KunButton
            variant="flat"
            size="sm"
            :color="issueCount ? 'warning' : 'success'"
          >
            <KunIcon
              :name="issueCount ? 'lucide:triangle-alert' : 'lucide:circle-check'"
              class="size-4"
            />
            <span class="tabular-nums">{{ issueCount || '无问题' }}</span>
          </KunButton>
        </template>
        <div class="max-h-[60vh] overflow-y-auto p-1">
          <p v-if="!issueCount" class="text-default-500 px-3 py-4 text-center text-sm">
            攻略图没有发现问题。
          </p>
          <button
            v-for="(issue, i) in doc.issues.value"
            :key="i"
            type="button"
            class="hover:bg-default-100 flex w-full items-start gap-2 rounded-lg px-3 py-2 text-left text-sm"
            @click="jump(issue)"
          >
            <KunIcon name="lucide:circle-alert" class="text-warning-500 mt-0.5 size-4 shrink-0" />
            <span class="flex-1">{{ issue.message }}</span>
            <KunIcon
              v-if="issue.target"
              name="lucide:corner-down-right"
              class="text-default-400 mt-0.5 size-4 shrink-0"
            />
          </button>
        </div>
      </KunPopover>

      <KunButton
        v-if="props.entityId"
        variant="flat"
        size="sm"
        :color="props.changeCount ? 'primary' : 'default'"
        :disabled="!props.changeCount"
        @click="emit('changes')"
      >
        <KunIcon name="lucide:file-diff" class="size-4" />
        <span class="hidden sm:inline">查看更改</span>
        <span v-if="props.changeCount" class="tabular-nums">{{ props.changeCount }}</span>
      </KunButton>

      <KunTooltip
        :text="issueCount ? `还有 ${issueCount} 处需要修正` : props.submitHint"
        position="bottom"
      >
        <KunButton
          color="primary"
          size="sm"
          :disabled="!props.canSubmit"
          @click="emit('submit')"
        >
          <KunIcon name="lucide:send" class="size-4" />
          提交
        </KunButton>
      </KunTooltip>

      <KunDropdown :items="moreItems" position="bottom-end" @select="onMore">
        <template #trigger>
          <KunButton variant="light" size="sm" is-icon-only aria-label="更多操作">
            <KunIcon name="lucide:ellipsis" class="size-4" />
          </KunButton>
        </template>
      </KunDropdown>

      <LayoutAccountMenu />
    </div>
  </header>
</template>
