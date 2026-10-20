<script setup lang="ts">
import type { KunDropdownItem } from '@kungal/ui-vue'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const props = defineProps<{
  family: EditFamily
  entityId: string
  name: string
}>()

const emit = defineEmits<{
  'open-nav': []
  'open-changes': []
  'open-submit': []
}>()

const session = useWorkspaceSession()
const { link } = useReturnLink()
const meta = computed(() => ENTITY_META[props.family])

const moreItems = computed(() => [
  {
    key: 'view',
    label: '查看资料页',
    icon: 'lucide:eye'
  },
  {
    key: 'history',
    label: '修订历史',
    icon: 'lucide:history'
  },
  { type: 'separator' as const },
  {
    key: 'discard',
    label: '放弃全部修改',
    icon: 'lucide:trash-2',
    color: 'danger' as const,
    disabled: !session.isDirty.value
  }
])

const onMore = async (item: KunDropdownItem) => {
  if (item.key === 'view') {
    window.open(`${meta.value.path}/${props.entityId}`, '_blank', 'noopener')
  } else if (item.key === 'history') {
    window.open(
      `/history/${props.family}/${props.entityId}`,
      '_blank',
      'noopener'
    )
  } else if (item.key === 'discard') {
    const ok = await useKunAlert({
      title: '放弃全部修改',
      message: `这会清空 ${session.dirty.value.length} 个条目上所有未提交的修改。`,
      type: 'warning',
      confirmText: '放弃',
      cancelText: '保留'
    })
    if (ok) {
      session.discardAll()
    }
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
        <KunIcon :name="meta.icon" class="size-4" />
        编辑{{ meta.label }}
      </span>
      <span class="truncate text-sm font-semibold">{{ props.name }}</span>
      <KunCopy
        :text="props.entityId"
        size="xs"
        variant="light"
        class-name="hidden md:inline-flex"
      />
    </div>

    <div class="flex shrink-0 items-center gap-1 md:gap-2">
      <KunButton
        v-if="link"
        :href="link.url"
        variant="light"
        size="sm"
        class-name="hidden xl:inline-flex"
      >
        <KunIcon name="lucide:arrow-left" class="size-4" />
        返回{{ link.label }}
      </KunButton>

      <KunButton
        variant="flat"
        size="sm"
        :color="session.isDirty.value ? 'primary' : 'default'"
        :disabled="!session.isDirty.value"
        @click="emit('open-changes')"
      >
        <KunIcon name="lucide:file-diff" class="size-4" />
        <span class="hidden sm:inline">查看更改</span>
        <span v-if="session.changeCount.value" class="tabular-nums">
          {{ session.changeCount.value }}
        </span>
      </KunButton>

      <KunTooltip
        :text="
          session.issueCount.value
            ? `还有 ${session.issueCount.value} 处需要修正`
            : session.blockedCount.value
              ? `${session.blockedCount.value} 项修改你没有权限提交`
              : session.willAutomerge.value
                ? '提交后直接生效（Ctrl/⌘ + S）'
                : '提交后进入审核（Ctrl/⌘ + S）'
        "
        position="bottom"
      >
        <KunButton
          color="primary"
          size="sm"
          :disabled="!session.canSubmit.value"
          @click="emit('open-submit')"
        >
          <KunIcon name="lucide:send" class="size-4" />
          提交
        </KunButton>
      </KunTooltip>

      <KunDropdown :items="moreItems" position="bottom-end" @select="onMore">
        <template #trigger>
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="更多操作"
          >
            <KunIcon name="lucide:ellipsis" class="size-4" />
          </KunButton>
        </template>
      </KunDropdown>

      <LayoutAccountMenu />
    </div>
  </header>
</template>
