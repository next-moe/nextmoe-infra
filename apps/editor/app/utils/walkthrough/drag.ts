import { ref } from 'vue'
import { useWalkthroughDocument } from '~/composables/useWalkthroughDocument'

type Dragging =
  | { kind: 'step'; segment: string; index: number }
  | { kind: 'segment'; segment: string }

const dragging = ref<Dragging | null>(null)
const stepOver = ref<{ segment: string; index: number; after: boolean } | null>(
  null
)
const segmentOver = ref<string | null>(null)

const MIME = 'application/x-nextmoe-walkthrough'

const lowerHalf = (event: DragEvent) => {
  const box = (event.currentTarget as HTMLElement).getBoundingClientRect()
  return event.clientY > box.top + box.height / 2
}

export const useStepDrag = () => {
  const doc = useWalkthroughDocument()

  const begin = (event: DragEvent, value: Dragging) => {
    dragging.value = value
    event.dataTransfer?.setData(MIME, JSON.stringify(value))
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
  }

  const end = () => {
    dragging.value = null
    stepOver.value = null
    segmentOver.value = null
  }

  const startStep = (event: DragEvent, segment: string, index: number) =>
    begin(event, { kind: 'step', segment, index })

  const startSegment = (event: DragEvent, segment: string) =>
    begin(event, { kind: 'segment', segment })

  const overStep = (event: DragEvent, segment: string, index: number) => {
    if (dragging.value?.kind !== 'step') return
    event.preventDefault()
    event.stopPropagation()
    stepOver.value = { segment, index, after: lowerHalf(event) }
    segmentOver.value = null
  }

  const moveStep = (
    from: { segment: string; index: number },
    to: { segment: string; index: number }
  ) =>
    doc.edit((d) => {
      const source = d.segments.find((s) => s.key === from.segment)
      const target = d.segments.find((s) => s.key === to.segment)
      if (!source || !target) return
      const [step] = source.steps.splice(from.index, 1)
      if (!step) return
      let at = to.index
      if (source === target && from.index < to.index) at--
      target.steps.splice(
        Math.max(0, Math.min(at, target.steps.length)),
        0,
        step
      )
    })

  const dropOnStep = (event: DragEvent, segment: string, index: number) => {
    const current = dragging.value
    if (current?.kind !== 'step') return
    event.preventDefault()
    event.stopPropagation()
    const at = index + (lowerHalf(event) ? 1 : 0)
    if (!(
      current.segment === segment &&
      (at === current.index || at === current.index + 1)
    )) {
      moveStep(current, { segment, index: at })
    }
    end()
  }

  const overSegment = (event: DragEvent, segment: string) => {
    if (!dragging.value) return
    event.preventDefault()
    if (dragging.value.kind === 'segment' && dragging.value.segment === segment)
      return
    segmentOver.value = segment
    stepOver.value = null
  }

  const dropOnSegment = (event: DragEvent, segment: string) => {
    const current = dragging.value
    if (!current) return
    event.preventDefault()
    if (current.kind === 'step') {
      const target = doc.chart.value.segments.find((s) => s.key === segment)
      if (target) moveStep(current, { segment, index: target.steps.length })
    } else if (current.segment !== segment) {
      const after = lowerHalf(event)
      doc.edit((d) => {
        const from = d.segments.findIndex((s) => s.key === current.segment)
        const [moved] = d.segments.splice(from, 1)
        const to = d.segments.findIndex((s) => s.key === segment)
        if (moved) d.segments.splice(to + (after ? 1 : 0), 0, moved)
      })
    }
    end()
  }

  return {
    dragging,
    stepOver,
    segmentOver,
    startStep,
    startSegment,
    overStep,
    dropOnStep,
    overSegment,
    dropOnSegment,
    end
  }
}
