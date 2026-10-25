<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WtNodeType } from '#shared/types/walkthrough'
import { WT_ENDING_LABEL, wtTone } from '~/constants/walkthrough'
import {
  layoutChart,
  type WtGraphEdge,
  type WtGraphLayout,
  type WtGraphNode
} from '~/utils/walkthrough/graph'

const LAYOUT_DELAY_MS = 120

const doc = useWalkthroughDocument()

const layout = shallowRef<WtGraphLayout>(layoutChart(doc.chart.value))
let timer: ReturnType<typeof setTimeout> | null = null
watch(doc.chart, (chart) => {
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => {
    layout.value = layoutChart(chart)
  }, LAYOUT_DELAY_MS)
})
onBeforeUnmount(() => timer && clearTimeout(timer))

const viewport = ref<HTMLElement | null>(null)
const size = computed(() => ({
  width: layout.value.width,
  height: layout.value.height
}))
const view = useGraphViewport(viewport, size)

const selectedSegment = computed(() =>
  doc.selection.value?.kind === 'segment' ? doc.selection.value.key : null
)
const selectedStep = computed(() =>
  doc.selection.value?.kind === 'segment' ? doc.selection.value.step : undefined
)
const selectedNode = computed(() => {
  const s = doc.selection.value
  return s &&
    s.kind !== 'segment' &&
    s.kind !== 'character' &&
    s.kind !== 'section'
    ? `${s.kind}:${s.key}`
    : null
})

const routeKeyOfSegment = (key: string) => {
  const seg = doc.chart.value.segments.find((s) => s.key === key)
  if (!seg) return null
  const ending =
    seg.to.node_type === 'ending'
      ? doc.chart.value.endings.find((e) => e.key === seg.to.key)
      : null
  return ending?.characters[0] ?? null
}

const dimmed = (edge: WtGraphEdge) => {
  const filter = doc.routeFilter.value
  if (!filter || !edge.segmentKey) return false
  const route = routeKeyOfSegment(edge.segmentKey)
  return filter === '__common__' ? route !== null : route !== filter
}

const edgeActive = (edge: WtGraphEdge) => {
  if (edge.segmentKey !== selectedSegment.value) return false
  const step = selectedStep.value
  return step === undefined || (step >= edge.stepStart && step <= edge.stepEnd)
}

const selectedEnds = computed(() => {
  const seg = doc.chart.value.segments.find(
    (s) => s.key === selectedSegment.value
  )
  return seg
    ? new Set([
        `${seg.from.node_type}:${seg.from.key}`,
        `${seg.to.node_type}:${seg.to.key}`
      ])
    : new Set<string>()
})

const nodeActive = (node: WtGraphNode) =>
  node.id === selectedNode.value ||
  (node.segmentKey !== null
    ? node.segmentKey === selectedSegment.value
    : selectedEnds.value.has(node.id))

const NODE_ICON: Record<WtNodeType, string> = {
  entry: 'lucide:flag',
  save: 'lucide:save',
  ending: 'lucide:trophy',
  collect: 'lucide:check-check',
  open: 'lucide:ellipsis'
}

const endingType = (node: WtGraphNode) =>
  node.kind === 'ending'
    ? WT_ENDING_LABEL[
        doc.chart.value.endings.find((e) => e.key === node.key)?.ending_type ??
          'other'
      ]
    : ''

const clickNode = (node: WtGraphNode) => {
  if (view.wasDrag()) return
  if (node.segmentKey) {
    doc.select({ kind: 'segment', key: node.segmentKey }, true)
  } else if (node.key) {
    const seg =
      doc.chart.value.segments.find(
        (s) => s.from.node_type === node.kind && s.from.key === node.key
      ) ??
      doc.chart.value.segments.find(
        (s) => s.to.node_type === node.kind && s.to.key === node.key
      )
    if (seg) doc.select({ kind: 'segment', key: seg.key }, true)
  }
}

const clickEdge = (edge: WtGraphEdge) => {
  if (view.wasDrag() || !edge.segmentKey) return
  doc.select(
    {
      kind: 'segment',
      key: edge.segmentKey,
      step: edge.stepStart < edge.stepEnd ? edge.stepStart : undefined
    },
    true
  )
}

const clickBackground = () => {
  if (!view.wasDrag()) doc.select(null)
}

watch([selectedSegment, selectedStep], () => {
  const edge = layout.value.edges.find(edgeActive)
  if (edge) view.reveal(edge.labelX, edge.labelY, 80, 40)
})

const hovered = ref<WtGraphEdge | null>(null)
</script>

<template>
  <div class="bg-background relative flex h-full min-h-0 flex-col">
    <div
      ref="viewport"
      :class="
        cn(
          'relative min-h-0 flex-1 touch-none overflow-hidden select-none',
          view.isPanning.value ? 'cursor-grabbing' : 'cursor-grab'
        )
      "
      @pointerdown="view.onPointerDown"
      @pointermove="view.onPointerMove"
      @pointerup="view.onPointerUp"
      @pointercancel="view.onPointerUp"
      @wheel="view.onWheel"
      @click.self="clickBackground"
    >
      <svg
        class="text-default-200 pointer-events-none absolute inset-0 size-full"
        aria-hidden="true"
      >
        <defs>
          <pattern
            id="wt-grid"
            width="24"
            height="24"
            patternUnits="userSpaceOnUse"
          >
            <circle cx="1" cy="1" r="1" class="fill-current" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#wt-grid)" />
      </svg>

      <div
        :class="
          cn(
            'absolute top-0 left-0 origin-top-left',
            view.animate.value && 'transition-transform duration-200 ease-out'
          )
        "
        :style="{
          transform: view.transform.value,
          width: `${layout.width}px`,
          height: `${layout.height}px`
        }"
        @click.self="clickBackground"
      >
        <svg
          :width="layout.width"
          :height="layout.height"
          class="absolute inset-0 overflow-visible"
        >
          <g
            v-for="edge in layout.edges"
            :key="edge.id"
            :class="cn('transition-opacity', dimmed(edge) && 'opacity-20')"
          >
            <path
              :d="edge.path"
              fill="none"
              :class="
                cn(
                  edgeActive(edge)
                    ? 'stroke-primary-500'
                    : edge.segmentKey
                      ? wtTone(edge.tone).stroke
                      : 'stroke-default-300',
                  !edge.segmentKey && '[stroke-dasharray:4_4]'
                )
              "
              :stroke-width="edgeActive(edge) ? 3 : 1.75"
            />
            <path
              v-if="edge.segmentKey"
              :d="edge.path"
              fill="none"
              stroke="transparent"
              stroke-width="14"
              class="pointer-events-auto cursor-pointer"
              @click.stop="clickEdge(edge)"
              @pointerenter="hovered = edge"
              @pointerleave="hovered = null"
            />
            <polygon
              points="-8,-4.5 0,0 -8,4.5"
              :transform="`translate(${edge.head.x} ${edge.head.y}) rotate(${edge.head.angle})`"
              :class="
                edgeActive(edge)
                  ? 'fill-primary-500'
                  : edge.segmentKey
                    ? wtTone(edge.tone).fill
                    : 'fill-default-300'
              "
            />
            <text
              v-if="edge.stepCount"
              :x="edge.labelX"
              :y="edge.labelY"
              :class="
                cn(
                  'pointer-events-none text-[11px] tabular-nums',
                  edgeActive(edge)
                    ? 'fill-primary-600 font-semibold'
                    : 'fill-default-400'
                )
              "
            >
              {{ edge.stepCount }}
            </text>
          </g>
        </svg>

        <button
          v-for="node in layout.nodes"
          :key="node.id"
          type="button"
          tabindex="-1"
          :class="
            cn(
              'shadow-kun-sm absolute flex items-center gap-1.5 overflow-hidden rounded-lg border px-2 text-left text-xs transition-[box-shadow,border-color]',
              node.kind === 'ending'
                ? cn('bg-content1 border-2', wtTone(node.tone).border)
                : node.kind === 'save'
                  ? 'bg-success-50 border-success-300 text-success-800'
                  : node.kind === 'entry'
                    ? 'bg-foreground text-background border-foreground font-semibold'
                    : 'bg-default-100 border-default-200 text-default-500',
              nodeActive(node) && 'ring-primary-400 ring-2 ring-offset-1'
            )
          "
          :style="{
            left: `${node.x - node.width / 2}px`,
            top: `${node.y - node.height / 2}px`,
            width: `${node.width}px`,
            height: `${node.height}px`
          }"
          :title="node.label"
          @click.stop="clickNode(node)"
        >
          <KunIcon
            :name="NODE_ICON[node.kind]"
            class="size-3.5 shrink-0 opacity-70"
          />
          <span class="min-w-0 flex-1">
            <span class="block truncate">{{ node.label }}</span>
            <span
              v-if="node.kind === 'ending'"
              class="text-default-400 block truncate text-[10px]"
            >
              {{ endingType(node) }}
            </span>
          </span>
        </button>
      </div>

      <div
        v-if="hovered && hovered.firstText"
        class="bg-content1 border-default-200 text-default-600 shadow-kun-md pointer-events-none absolute bottom-14 left-1/2 max-w-[80%] -translate-x-1/2 truncate rounded-lg border px-3 py-1.5 text-xs"
      >
        {{ hovered.firstText
        }}<template v-if="hovered.stepCount > 1">
          …共 {{ hovered.stepCount }} 步</template
        >
      </div>

      <div
        v-if="!doc.chart.value.segments.length"
        class="text-default-400 pointer-events-none absolute inset-0 flex items-center justify-center text-sm"
      >
        添加路线段后，这里会画出攻略图
      </div>
    </div>

    <div
      class="bg-content1 border-default-200 shadow-kun-sm absolute right-3 bottom-3 flex items-center gap-0.5 rounded-lg border p-0.5"
    >
      <KunTooltip text="缩小" position="top">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="缩小"
          @click="view.zoomBy(1 / 1.25)"
        >
          <KunIcon name="lucide:minus" class="size-4" />
        </KunButton>
      </KunTooltip>
      <span class="text-default-500 w-11 text-center text-xs tabular-nums"
        >{{ Math.round(view.scale.value * 100) }}%</span
      >
      <KunTooltip text="放大" position="top">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="放大"
          @click="view.zoomBy(1.25)"
        >
          <KunIcon name="lucide:plus" class="size-4" />
        </KunButton>
      </KunTooltip>
      <KunTooltip text="适应窗口" position="top">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="适应窗口"
          @click="view.fit()"
        >
          <KunIcon name="lucide:scan" class="size-4" />
        </KunButton>
      </KunTooltip>
    </div>
  </div>
</template>
