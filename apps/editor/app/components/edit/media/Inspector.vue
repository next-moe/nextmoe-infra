<script setup lang="ts">
import {
  COVER_KINDS,
  type MediaKind,
  type MediaTile
} from '~/utils/mediaGallery'

const props = defineProps<{
  tile: MediaTile
  kind: MediaKind
  position: number
  total: number
  image: { url: string; thumbhash: string | null } | null
  canEdit: boolean
  canHide: boolean
  canReorder: boolean
  canPin: boolean
  anyPinned: boolean
}>()

const emit = defineEmits<{
  close: []
  preview: []
  pin: []
  hide: []
  remove: []
  revert: []
  move: [to: number]
  update: [key: string, value: unknown]
}>()

const pinned = computed(() => Boolean(props.tile.row.portrait_pinned))
const text = computed({
  get: () => String(props.tile.row.caption ?? ''),
  set: (next: string | number) => emit('update', 'caption', String(next))
})
const coverKind = computed({
  get: () => String(props.tile.row.kind ?? ''),
  set: (next: string) => emit('update', 'kind', next)
})
const coverKindOptions = computed(() => {
  const current = String(props.tile.row.kind ?? '')
  return COVER_KINDS.some((entry) => entry.value === current)
    ? COVER_KINDS
    : [...COVER_KINDS, { value: current, label: current }]
})
const grade = (axis: 'sexual' | 'violence') =>
  computed({
    get: () => Number(props.tile.row[axis] ?? 0),
    set: (next: number) => emit('update', axis, next)
  })
const sexual = grade('sexual')
const violence = grade('violence')
const changedMember = (key: string) =>
  Boolean(props.tile.baseRow) &&
  (props.tile.baseRow?.[key] ?? null) !== (props.tile.row[key] ?? null) &&
  Boolean(props.tile.baseRow?.[key] || props.tile.row[key])

const origin = computed(() =>
  props.tile.lane === 'human'
    ? '人工上传的图片，可以修改、删除和调整位置。'
    : `由 ${props.tile.source ?? '资料源'} 导入。改过的分级、${props.kind === 'cover' ? '类型' : '说明'}和位置不会被导入任务覆盖；隐藏后所有页面都不再显示它，可以随时恢复。`
)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header
      class="border-default-200 flex shrink-0 items-center gap-2 border-b px-4 py-3"
    >
      <span class="font-semibold">第 {{ props.position }} 张</span>
      <span class="text-default-400 text-sm tabular-nums">
        / {{ props.total }}
      </span>
      <WorkspaceSourceBadge
        :human="props.tile.lane === 'human'"
        :source="props.tile.source"
      />
      <KunChip v-if="props.tile.isNew" size="xs" variant="flat" color="success">
        新上传
      </KunChip>
      <KunChip
        v-else-if="props.tile.changed"
        size="xs"
        variant="flat"
        color="primary"
      >
        已修改
      </KunChip>
      <span class="flex-1" />
      <KunButton
        variant="light"
        size="sm"
        is-icon-only
        aria-label="关闭"
        @click="emit('close')"
      >
        <KunIcon name="lucide:x" class="size-4" />
      </KunButton>
    </header>

    <div class="min-h-0 flex-1 space-y-5 overflow-y-auto p-4">
      <button
        type="button"
        class="group bg-default-100 border-default-200 relative flex min-h-40 w-full items-center justify-center overflow-hidden rounded-lg border"
        aria-label="查看大图"
        @click="emit('preview')"
      >
        <KunImage
          v-if="props.image"
          :key="props.tile.hash"
          :src="props.image.url"
          :thumbhash="props.image.thumbhash ?? undefined"
          alt=""
          object-fit="contain"
          class-name="block"
          image-class-name="max-h-[55vh] w-auto max-w-full"
        />
        <span
          class="bg-background/85 text-default-700 absolute right-2 bottom-2 inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
        >
          <KunIcon name="lucide:maximize-2" class="size-3.5" />
          查看大图
        </span>
      </button>

      <p class="text-default-500 text-xs leading-5">{{ origin }}</p>
      <p
        v-if="props.kind === 'cover' && !props.anyPinned"
        class="text-default-500 bg-default-100 rounded-lg px-3 py-2 text-xs leading-5"
      >
        还没有指定主封面，页面会自动挑选最合适的竖版图片。想固定某一张，点「设为主封面」。
      </p>

      <div class="flex flex-wrap gap-2">
        <KunButton
          v-if="props.kind === 'cover'"
          size="sm"
          :variant="pinned ? 'flat' : 'bordered'"
          :color="pinned ? 'primary' : 'default'"
          :disabled="!props.canPin || props.tile.hidden"
          @click="emit('pin')"
        >
          <KunIcon name="lucide:star" class="size-4" />
          {{ pinned ? '取消主封面' : '设为主封面' }}
        </KunButton>
        <KunButton
          v-if="props.tile.lane === 'upstream'"
          size="sm"
          variant="bordered"
          :color="props.tile.hidden ? 'primary' : 'default'"
          :disabled="!props.canHide || !props.tile.identity"
          @click="emit('hide')"
        >
          <KunIcon
            :name="props.tile.hidden ? 'lucide:eye' : 'lucide:eye-off'"
            class="size-4"
          />
          {{ props.tile.hidden ? '恢复显示' : '隐藏' }}
        </KunButton>
        <KunButton
          v-else
          size="sm"
          variant="bordered"
          color="danger"
          :disabled="!props.canEdit"
          @click="emit('remove')"
        >
          <KunIcon name="lucide:trash-2" class="size-4" />
          删除
        </KunButton>
        <KunButton
          v-if="props.tile.changed && !props.tile.isNew"
          size="sm"
          variant="light"
          color="default"
          @click="emit('revert')"
        >
          <KunIcon name="lucide:undo-2" class="size-4" />
          撤销修改
        </KunButton>
      </div>

      <div v-if="props.canReorder" class="space-y-1.5">
        <p class="text-default-600 text-sm">位置</p>
        <KunButtonGroup aria-label="调整位置">
          <KunButton
            size="sm"
            variant="bordered"
            :disabled="props.position === 1"
            @click="emit('move', 0)"
          >
            <KunIcon name="lucide:chevrons-left" class="size-4" />
            最前
          </KunButton>
          <KunButton
            size="sm"
            variant="bordered"
            is-icon-only
            aria-label="前移一位"
            :disabled="props.position === 1"
            @click="emit('move', props.position - 2)"
          >
            <KunIcon name="lucide:chevron-left" class="size-4" />
          </KunButton>
          <KunButton
            size="sm"
            variant="bordered"
            is-icon-only
            aria-label="后移一位"
            :disabled="props.position === props.total"
            @click="emit('move', props.position + 1)"
          >
            <KunIcon name="lucide:chevron-right" class="size-4" />
          </KunButton>
          <KunButton
            size="sm"
            variant="bordered"
            :disabled="props.position === props.total"
            @click="emit('move', props.total)"
          >
            最后
            <KunIcon name="lucide:chevrons-right" class="size-4" />
          </KunButton>
        </KunButtonGroup>
      </div>

      <div v-if="props.kind === 'cover'" class="space-y-1.5">
        <div class="flex items-center gap-1.5 text-sm">
          <span class="text-default-600">类型</span>
          <span
            v-if="changedMember('kind')"
            class="bg-primary-500 size-1.5 rounded-full"
          />
        </div>
        <KunRadioGroup
          v-model="coverKind"
          :options="coverKindOptions"
          variant="pill"
          orientation="horizontal"
          size="sm"
          :disabled="!props.canEdit"
          aria-label="封面类型"
        />
      </div>
      <div v-else class="space-y-1.5">
        <div class="flex items-center gap-1.5 text-sm">
          <span class="text-default-600">说明</span>
          <span
            v-if="changedMember('caption')"
            class="bg-primary-500 size-1.5 rounded-full"
          />
        </div>
        <KunInput
          v-model="text"
          placeholder="例如：OP、标题画面、战斗场景"
          aria-label="截图说明"
          :disabled="!props.canEdit"
        />
      </div>

      <EditMediaGrade
        v-model="sexual"
        axis="sexual"
        :disabled="!props.canEdit"
        :changed="changedMember('sexual')"
      />
      <EditMediaGrade
        v-model="violence"
        axis="violence"
        :disabled="!props.canEdit"
        :changed="changedMember('violence')"
      />

      <div
        class="border-default-200 text-default-500 space-y-1 border-t pt-4 text-xs"
      >
        <div class="flex items-center justify-between gap-2">
          <span>图片哈希</span>
          <KunCopy
            :text="props.tile.hash"
            name="复制"
            size="xs"
            variant="light"
          />
        </div>
        <p class="font-mono break-all">{{ props.tile.hash }}</p>
        <a
          v-if="props.image"
          :href="props.image.url"
          target="_blank"
          rel="noopener"
          class="text-primary-600 inline-flex items-center gap-1 hover:underline"
        >
          <KunIcon name="lucide:external-link" class="size-3.5" />
          在新标签页打开原图
        </a>
      </div>
    </div>
  </div>
</template>
