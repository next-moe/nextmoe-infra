<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  MEDIA_FIELDS,
  asHashList,
  asMediaRows,
  buildMediaTiles,
  contextMediaRows,
  moveHash,
  orderResetPending,
  orderValueFor,
  removedHumanRows,
  setPinned,
  setRowMember,
  type MediaKind,
  type MediaRow,
  type MediaState,
  type MediaTile
} from '~/utils/mediaGallery'

interface UploadItem {
  id: number
  name: string
  state: 'uploading' | 'failed'
  error?: string
}

const props = defineProps<{ kind: MediaKind }>()

const context = useEditorContext()
const { editor, images } = context
const session = useWorkspaceSession()
const nav = useWorkspaceNav()
const api = useEditorApi()

const keys = MEDIA_FIELDS[props.kind]
const label = props.kind === 'cover' ? '封面' : '截图'
const entity = session.open('work', context.entityId.value)

const rawField = computed(() =>
  entity.workspace.editContext.context.value?.fields?.find(
    (field) => field.key === keys.human
  )
)

const state = computed<MediaState>(() => ({
  human: asMediaRows(editor.draft.value[keys.human]),
  baseHuman: asMediaRows(editor.base.value[keys.human]),
  upstream: asMediaRows(editor.draft.value[keys.upstream]),
  baseUpstream: asMediaRows(editor.base.value[keys.upstream]),
  suppressed: asHashList(editor.draft.value[keys.suppressed]),
  baseSuppressed: asHashList(editor.base.value[keys.suppressed]),
  order: asHashList(editor.draft.value[keys.order]),
  baseOrder: asHashList(editor.base.value[keys.order]),
  context: contextMediaRows(rawField.value)
}))

const tiles = computed(() => buildMediaTiles(state.value))
const removed = computed(() => removedHumanRows(state.value))
const resetPending = computed(() => orderResetPending(state.value))

const can = (key: string) =>
  !context.readonly &&
  editor.fieldByKey.value.has(key) &&
  Boolean(editor.capabilityByKey.value.get(key)?.can_propose)
const canUpload = computed(() => can(keys.human))
const canRegrade = computed(() => can(keys.upstream))
const canHide = computed(() => can(keys.suppressed))
const canReorder = computed(() => can(keys.order))
const capacity = computed(
  () => editor.fieldByKey.value.get(keys.human)?.max_elements ?? 0
)

const canEditTile = (tile: MediaTile) =>
  tile.lane === 'human' ? canUpload.value : canRegrade.value

const imageOf = (hash: string) =>
  images.value[hash] ?? { url: imageUrl(hash), thumbhash: null }

const changedRows = (next: MediaRow[], prev: MediaRow[]) =>
  next.length !== prev.length || next.some((row, i) => row !== prev[i])

const setHuman = (rows: MediaRow[]) => editor.setValue(keys.human, rows)
const setUpstream = (rows: MediaRow[]) => editor.setValue(keys.upstream, rows)

const selectedHash = computed({
  get: () => nav.item.value,
  set: (hash: string | null) => {
    nav.item.value = hash
  }
})
const selectedIndex = computed(() =>
  tiles.value.findIndex((tile) => tile.hash === selectedHash.value)
)
const selected = computed(() => tiles.value[selectedIndex.value] ?? null)

const reorder = (hash: string, to: number) => {
  const next = moveHash(
    tiles.value.map((tile) => tile.hash),
    hash,
    to
  )
  editor.setValue(keys.order, orderValueFor(next, state.value))
}

const resetOrder = () => editor.setValue(keys.order, [])

const updateTile = (tile: MediaTile, key: string, value: unknown) => {
  if (tile.lane === 'human') {
    setHuman(setRowMember(state.value.human, tile.index, key, value, true))
  } else {
    setUpstream(
      setRowMember(state.value.upstream, tile.index, key, value, false)
    )
  }
}

const togglePin = (tile: MediaTile) => {
  const next = setPinned(
    state.value,
    tile.row.portrait_pinned ? null : tile.hash
  )
  if (changedRows(next.human, state.value.human)) {
    setHuman(next.human)
  }
  if (changedRows(next.upstream, state.value.upstream)) {
    setUpstream(next.upstream)
  }
}

const toggleHidden = (tile: MediaTile) => {
  if (!tile.identity) {
    return
  }
  const current = new Set(state.value.suppressed)
  if (current.has(tile.identity)) {
    current.delete(tile.identity)
  } else {
    current.add(tile.identity)
  }
  editor.setValue(keys.suppressed, sortedIdentities(current))
}

const removeTile = (tile: MediaTile) => {
  setHuman(state.value.human.filter((_, i) => i !== tile.index))
  if (selectedHash.value === tile.hash) {
    selectedHash.value = null
  }
}

const restoreRemoved = (row: MediaRow) => setHuman([...state.value.human, row])

const revertTile = (tile: MediaTile) => {
  if (tile.lane === 'human') {
    if (tile.baseRow) {
      setHuman(
        state.value.human.map((row, i) =>
          i === tile.index ? tile.baseRow! : row
        )
      )
    }
    return
  }
  if (tile.baseRow) {
    setUpstream(
      state.value.upstream.map((row, i) =>
        i === tile.index ? tile.baseRow! : row
      )
    )
  }
  if (tile.identity && tile.hidden !== tile.baseHidden) {
    toggleHidden(tile)
  }
}

const uploads = ref<UploadItem[]>([])
let uploadSeq = 0

const failUpload = (name: string, error: string) => {
  uploads.value = [
    ...uploads.value,
    { id: ++uploadSeq, name, state: 'failed', error }
  ]
}

const uploadFiles = async (files: File[]) => {
  for (const file of files) {
    if (!isImageFile(file)) {
      failUpload(file.name, '只支持 PNG、JPEG、WebP、AVIF 和 GIF 图片')
      continue
    }
    if (file.size > EDIT_IMAGE_MAX_BYTES) {
      failUpload(file.name, '图片不能超过 20 MB')
      continue
    }
    if (capacity.value && state.value.human.length >= capacity.value) {
      failUpload(file.name, `人工上传的${label}最多 ${capacity.value} 张`)
      continue
    }
    const item: UploadItem = {
      id: ++uploadSeq,
      name: file.name,
      state: 'uploading'
    }
    uploads.value = [...uploads.value, item]
    try {
      const image = await uploadEditImage(api, keys.preset, file)
      images.value = {
        ...images.value,
        [image.hash]: { url: image.url, thumbhash: image.thumbhash ?? null }
      }
      const known = tiles.value.find((tile) => tile.hash === image.hash)
      if (known) {
        useKunMessage(
          known.lane === 'human'
            ? `这张${label}已经在列表里了`
            : `这张${label}已由 ${known.source ?? '资料源'} 导入，无需重复上传`,
          'info'
        )
      } else {
        setHuman([...state.value.human, { image_hash: image.hash }])
      }
      selectedHash.value = image.hash
      uploads.value = uploads.value.filter((entry) => entry.id !== item.id)
    } catch (error) {
      uploads.value = uploads.value.map((entry) =>
        entry.id === item.id
          ? {
              ...entry,
              state: 'failed',
              error: editProblemMessage(parseEditProblem(error))
            }
          : entry
      )
    }
  }
}

const dismissUpload = (id: number) => {
  uploads.value = uploads.value.filter((entry) => entry.id !== id)
}

const dragging = ref<string | null>(null)
const dropAt = ref<{ hash: string; side: 'before' | 'after' } | null>(null)

const onTileDragStart = (tile: MediaTile, event: DragEvent) => {
  if (!canReorder.value) {
    event.preventDefault()
    return
  }
  dragging.value = tile.hash
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('application/x-nextmoe-media', tile.hash)
  }
}

const onTileDragOver = (tile: MediaTile, event: DragEvent) => {
  if (!dragging.value) {
    return
  }
  event.preventDefault()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  dropAt.value = {
    hash: tile.hash,
    side: event.clientX < rect.left + rect.width / 2 ? 'before' : 'after'
  }
}

const endDrag = () => {
  dragging.value = null
  dropAt.value = null
}

const onTileDrop = (event: DragEvent) => {
  if (!dragging.value || !dropAt.value) {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  const sequence = tiles.value.map((tile) => tile.hash)
  const target = sequence.indexOf(dropAt.value.hash)
  reorder(dragging.value, target + (dropAt.value.side === 'after' ? 1 : 0))
  endDrag()
}

const dropSideOf = (tile: MediaTile) =>
  dropAt.value?.hash === tile.hash && dragging.value !== tile.hash
    ? dropAt.value.side
    : null

const fileHover = ref(0)
const onPageDragEnter = (event: DragEvent) => {
  if (hasDraggedFiles(event) && canUpload.value) {
    fileHover.value++
  }
}
const onPageDragLeave = (event: DragEvent) => {
  if (hasDraggedFiles(event) && fileHover.value > 0) {
    fileHover.value--
  }
}
const onPageDragOver = (event: DragEvent) => {
  if (hasDraggedFiles(event) && canUpload.value) {
    event.preventDefault()
  }
}
const onPageDrop = (event: DragEvent) => {
  if (!hasDraggedFiles(event)) {
    return
  }
  event.preventDefault()
  fileHover.value = 0
  if (canUpload.value) {
    uploadFiles(Array.from(event.dataTransfer?.files ?? []))
  }
}

const wide = ref(false)
let mediaQuery: MediaQueryList | null = null
const onWideChange = (event: MediaQueryListEvent) => {
  wide.value = event.matches
}
onMounted(() => {
  mediaQuery = window.matchMedia('(min-width: 1280px)')
  wide.value = mediaQuery.matches
  mediaQuery.addEventListener('change', onWideChange)
})
onBeforeUnmount(() => mediaQuery?.removeEventListener('change', onWideChange))

const drawerOpen = computed({
  get: () => !wide.value && selected.value !== null,
  set: (open: boolean) => {
    if (!open) {
      selectedHash.value = null
    }
  }
})

const lightboxOpen = ref(false)
const lightboxImages = computed(() =>
  tiles.value.map((tile, index) => ({
    src: imageOf(tile.hash).url,
    alt: `${label} ${index + 1}`
  }))
)

const anyPinned = computed(() =>
  tiles.value.some((tile) => tile.row.portrait_pinned)
)

const sourceSummary = computed(() => {
  const counts = new Map<string, number>()
  for (const tile of tiles.value) {
    const name = tile.lane === 'human' ? '人工' : (tile.source ?? '资料源')
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }
  const hidden = tiles.value.filter((tile) => tile.hidden).length
  return [
    ...[...counts].map(([name, count]) => `${name} ${count}`),
    ...(hidden ? [`已隐藏 ${hidden}`] : [])
  ].join(' · ')
})

const description = computed(() =>
  [
    canReorder.value ? '拖动图片调整顺序' : '',
    canUpload.value ? '把图片文件拖进这里即可上传' : '',
    '点选一张图片查看和编辑它的分级、来源与位置'
  ]
    .filter(Boolean)
    .join('，')
)

watch(
  () => tiles.value.map((tile) => tile.hash).join(','),
  () => {
    if (selectedHash.value && selectedIndex.value < 0) {
      selectedHash.value = null
    }
  }
)
</script>

<template>
  <div class="flex h-full min-h-0">
    <!-- eslint-disable-next-line vuejs-accessibility/no-static-element-interactions -->
    <div
      class="relative min-w-0 flex-1 overflow-y-auto"
      @dragenter="onPageDragEnter"
      @dragleave="onPageDragLeave"
      @dragover="onPageDragOver"
      @drop="onPageDrop"
    >
      <div class="mx-auto max-w-6xl space-y-4 px-4 py-5 md:px-8 md:py-6">
        <WorkspaceSectionHeader
          :title="label"
          :count="tiles.length"
          :changes="nav.changesBySection.value[nav.section.value] ?? 0"
          :description="description"
        >
          <KunButton
            v-if="canReorder && (state.order.length || resetPending)"
            variant="light"
            size="sm"
            :disabled="resetPending"
            @click="resetOrder"
          >
            <KunIcon name="lucide:rotate-ccw" class="size-4" />
            恢复默认顺序
          </KunButton>
          <KunFileInput
            v-if="canUpload"
            :accept="EDIT_IMAGE_ACCEPT"
            multiple
            :max-size="EDIT_IMAGE_MAX_BYTES"
            :trigger-text="`上传${label}`"
            trigger-icon="lucide:upload"
            trigger-variant="flat"
            trigger-color="primary"
            trigger-size="sm"
            @change="uploadFiles"
          />
        </WorkspaceSectionHeader>

        <div
          v-if="tiles.length || removed.length"
          class="text-default-500 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs"
        >
          <span>{{ sourceSummary }}</span>
          <span
            v-if="resetPending"
            class="text-warning-600 inline-flex items-center gap-1"
          >
            <KunIcon name="lucide:info" class="size-3.5" />
            提交后恢复资料源的默认顺序
          </span>
        </div>

        <ul v-if="uploads.length" class="space-y-1.5">
          <li
            v-for="item in uploads"
            :key="item.id"
            :class="
              cn(
                'flex items-center gap-2 rounded-lg px-3 py-2 text-sm',
                item.state === 'failed'
                  ? 'bg-danger-50 text-danger-700'
                  : 'bg-default-100 text-default-600'
              )
            "
          >
            <KunLoading v-if="item.state === 'uploading'" spinner />
            <KunIcon v-else name="lucide:circle-x" class="size-4 shrink-0" />
            <span class="min-w-0 flex-1 truncate">{{ item.name }}</span>
            <span v-if="item.error" class="text-xs">{{ item.error }}</span>
            <span v-else class="text-xs">正在上传…</span>
            <button
              v-if="item.state === 'failed'"
              type="button"
              class="hover:bg-danger-100 rounded p-0.5"
              aria-label="关闭"
              @click="dismissUpload(item.id)"
            >
              <KunIcon name="lucide:x" class="size-4" />
            </button>
          </li>
        </ul>

        <div
          v-if="tiles.length"
          :class="
            cn(
              'grid gap-4',
              props.kind === 'cover'
                ? 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5'
                : 'grid-cols-1 sm:grid-cols-2 2xl:grid-cols-3'
            )
          "
        >
          <EditMediaTile
            v-for="(tile, index) in tiles"
            :key="tile.hash"
            :tile="tile"
            :kind="props.kind"
            :position="index + 1"
            :image="imageOf(tile.hash)"
            :selected="tile.hash === selectedHash"
            :draggable="canReorder"
            :dragging="dragging === tile.hash"
            :drop-side="dropSideOf(tile)"
            @select="selectedHash = tile.hash"
            @move="reorder(tile.hash, index + ($event > 0 ? 2 : -1))"
            @dragstart="onTileDragStart(tile, $event)"
            @dragover="onTileDragOver(tile, $event)"
            @drop="onTileDrop"
            @dragend="endDrag"
          />
        </div>

        <WorkspaceEmpty
          v-else
          :icon="props.kind === 'cover' ? 'lucide:book-image' : 'lucide:images'"
          :title="`这部作品还没有${label}`"
          :description="
            canUpload
              ? `把图片文件拖进这里，或点击右上角上传${label}。`
              : `资料源导入${label}后会出现在这里。`
          "
          class-name="border-default-200 rounded-xl border border-dashed"
        />

        <section v-if="removed.length" class="space-y-2">
          <p class="text-default-500 text-xs">
            本次删除的{{ label }}（提交后生效）
          </p>
          <div class="flex flex-wrap gap-3">
            <div
              v-for="row in removed"
              :key="String(row.image_hash)"
              class="border-danger-300 w-28 overflow-hidden rounded-lg border border-dashed"
            >
              <KunImage
                :src="imageOf(String(row.image_hash)).url"
                alt=""
                :aspect-ratio="props.kind === 'cover' ? '3 / 4' : '16 / 9'"
                object-fit="cover"
                class-name="opacity-50 grayscale"
              />
              <button
                type="button"
                class="text-danger-600 hover:bg-danger-50 flex w-full items-center justify-center gap-1 py-1 text-xs"
                @click="restoreRemoved(row)"
              >
                <KunIcon name="lucide:undo-2" class="size-3.5" />
                撤销删除
              </button>
            </div>
          </div>
        </section>
      </div>

      <div
        v-if="fileHover > 0"
        class="border-primary-400 bg-primary-50/90 text-primary-700 pointer-events-none absolute inset-3 z-20 flex flex-col items-center justify-center gap-2 rounded-2xl border-2 border-dashed text-sm font-medium"
      >
        <KunIcon name="lucide:upload" class="size-8" />
        松开即可上传到{{ label }}
      </div>
    </div>

    <aside
      class="border-default-200 bg-content1 hidden w-96 shrink-0 border-l xl:block"
    >
      <EditMediaInspector
        v-if="selected"
        :tile="selected"
        :kind="props.kind"
        :position="selectedIndex + 1"
        :total="tiles.length"
        :image="imageOf(selected.hash)"
        :can-edit="canEditTile(selected)"
        :can-hide="canHide"
        :can-reorder="canReorder"
        :can-pin="canUpload && canRegrade"
        :any-pinned="anyPinned"
        @close="selectedHash = null"
        @preview="lightboxOpen = true"
        @pin="togglePin(selected)"
        @hide="toggleHidden(selected)"
        @remove="removeTile(selected)"
        @revert="revertTile(selected)"
        @move="reorder(selected.hash, $event)"
        @update="(key, value) => updateTile(selected!, key, value)"
      />
      <WorkspaceEmpty
        v-else
        icon="lucide:mouse-pointer-click"
        :title="`选择一张${label}`"
        description="在这里查看大图，修改分级、类型和位置，或隐藏不合适的资料源图片。"
        class-name="h-full"
      />
    </aside>

    <KunDrawer
      v-model="drawerOpen"
      placement="right"
      size="md"
      :aria-label="`${label}详情`"
      inner-class-name="p-0"
      :is-show-close-button="false"
    >
      <EditMediaInspector
        v-if="selected"
        :tile="selected"
        :kind="props.kind"
        :position="selectedIndex + 1"
        :total="tiles.length"
        :image="imageOf(selected.hash)"
        :can-edit="canEditTile(selected)"
        :can-hide="canHide"
        :can-reorder="canReorder"
        :can-pin="canUpload && canRegrade"
        :any-pinned="anyPinned"
        @close="selectedHash = null"
        @preview="lightboxOpen = true"
        @pin="togglePin(selected)"
        @hide="toggleHidden(selected)"
        @remove="removeTile(selected)"
        @revert="revertTile(selected)"
        @move="reorder(selected.hash, $event)"
        @update="(key, value) => updateTile(selected!, key, value)"
      />
    </KunDrawer>

    <KunLightbox
      :images="lightboxImages"
      :is-open="lightboxOpen"
      :initial-index="Math.max(selectedIndex, 0)"
      @update:is-open="lightboxOpen = $event"
    />
  </div>
</template>
