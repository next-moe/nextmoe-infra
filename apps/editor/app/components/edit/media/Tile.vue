<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  coverKindLabel,
  type MediaKind,
  type MediaTile
} from '~/utils/mediaGallery'

const SEXUAL = ['', '擦边', '露骨']
const VIOLENCE = ['', '暴力', '血腥']

const props = withDefaults(
  defineProps<{
    tile: MediaTile
    kind: MediaKind
    position: number
    image: { url: string; thumbhash: string | null } | null
    selected?: boolean
    draggable?: boolean
    dragging?: boolean
    dropSide?: 'before' | 'after' | null
  }>(),
  { selected: false, draggable: false, dragging: false, dropSide: null }
)

const emit = defineEmits<{
  select: []
  move: [delta: number]
  dragstart: [event: DragEvent]
  dragover: [event: DragEvent]
  drop: [event: DragEvent]
  dragend: []
}>()

const sexual = computed(() => Number(props.tile.row.sexual ?? 0))
const violence = computed(() => Number(props.tile.row.violence ?? 0))
const pinned = computed(() => Boolean(props.tile.row.portrait_pinned))
const caption = computed(() =>
  props.kind === 'cover'
    ? props.tile.row.kind
      ? coverKindLabel(props.tile.row.kind)
      : ''
    : String(props.tile.row.caption ?? '')
)

const onKeydown = (event: KeyboardEvent) => {
  if (!event.altKey || !props.draggable) {
    return
  }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
    event.preventDefault()
    emit('move', -1)
  } else if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
    event.preventDefault()
    emit('move', 1)
  }
}
</script>

<template>
  <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
  <div
    class="relative"
    @dragover="emit('dragover', $event)"
    @drop="emit('drop', $event)"
  >
    <span
      v-if="props.dropSide === 'before'"
      class="bg-primary-500 absolute inset-y-2 -left-2 z-10 w-1 rounded-full"
    />
    <span
      v-if="props.dropSide === 'after'"
      class="bg-primary-500 absolute inset-y-2 -right-2 z-10 w-1 rounded-full"
    />
    <button
      type="button"
      :draggable="props.draggable"
      :aria-pressed="props.selected"
      :aria-label="`第 ${props.position} 张${pinned ? '，主封面' : ''}${props.tile.hidden ? '，已隐藏' : ''}${props.draggable ? '。按 Alt + 方向键移动位置' : ''}`"
      :class="
        cn(
          'group bg-default-100 relative block w-full overflow-hidden rounded-xl border text-left transition',
          'focus-visible:ring-primary-400 focus-visible:ring-2 focus-visible:outline-none',
          props.selected
            ? 'border-primary-500 ring-primary-500 ring-2'
            : props.tile.isNew
              ? 'border-success-400'
              : props.tile.changed
                ? 'border-primary-300'
                : 'border-default-200 hover:border-default-400',
          props.draggable && 'cursor-grab active:cursor-grabbing',
          props.dragging && 'opacity-40'
        )
      "
      @click="emit('select')"
      @keydown="onKeydown"
      @dragstart="emit('dragstart', $event)"
      @dragend="emit('dragend')"
    >
      <KunImage
        v-if="props.image"
        :src="props.image.url"
        :thumbhash="props.image.thumbhash ?? undefined"
        alt=""
        :aspect-ratio="props.kind === 'cover' ? '3 / 4' : '16 / 9'"
        object-fit="cover"
        :class-name="cn(props.tile.hidden && 'grayscale')"
      />
      <div
        v-else
        :class="
          cn(
            'text-default-400 flex flex-col items-center justify-center gap-1 text-xs',
            props.kind === 'cover' ? 'aspect-[3/4]' : 'aspect-video'
          )
        "
      >
        <KunIcon name="lucide:image" class="size-6" />
      </div>

      <span
        class="bg-background/85 text-default-700 absolute top-2 left-2 rounded-md px-1.5 text-xs leading-5 font-medium tabular-nums"
      >
        {{ props.position }}
      </span>
      <WorkspaceSourceBadge
        :human="props.tile.lane === 'human'"
        :source="props.tile.source"
        class-name="absolute top-2 right-2"
      />

      <div
        v-if="props.tile.hidden"
        class="bg-background/70 text-default-700 absolute inset-0 flex flex-col items-center justify-center gap-1 text-xs font-medium"
      >
        <KunIcon name="lucide:eye-off" class="size-5" />
        已隐藏
      </div>

      <div
        class="absolute inset-x-2 bottom-2 flex flex-wrap items-center gap-1"
      >
        <span
          v-if="pinned"
          class="bg-primary-500 text-primary-foreground inline-flex h-5 items-center gap-0.5 rounded px-1.5 text-[11px] font-medium"
        >
          <KunIcon name="lucide:star" class="size-3" />
          主封面
        </span>
        <span
          v-if="props.tile.isNew"
          class="bg-success-500 text-success-foreground inline-flex h-5 items-center rounded px-1.5 text-[11px] font-medium"
        >
          新上传
        </span>
        <span
          v-if="sexual"
          :class="
            cn(
              'inline-flex h-5 items-center rounded px-1.5 text-[11px] font-medium',
              sexual === 2
                ? 'bg-danger-500 text-danger-foreground'
                : 'bg-warning-500 text-warning-foreground'
            )
          "
        >
          {{ SEXUAL[sexual] }}
        </span>
        <span
          v-if="violence"
          :class="
            cn(
              'inline-flex h-5 items-center rounded px-1.5 text-[11px] font-medium',
              violence === 2
                ? 'bg-danger-500 text-danger-foreground'
                : 'bg-warning-500 text-warning-foreground'
            )
          "
        >
          {{ VIOLENCE[violence] }}
        </span>
        <span class="flex-1" />
        <span
          v-if="props.tile.changed && !props.tile.isNew"
          class="bg-primary-500 ring-background size-2.5 rounded-full ring-2"
          aria-label="已修改"
        />
      </div>
    </button>
    <p
      v-if="caption"
      class="text-default-500 mt-1 truncate px-0.5 text-xs"
      :title="caption"
    >
      {{ caption }}
    </p>
  </div>
</template>
