const MIN_SCALE = 0.25
const MAX_SCALE = 2
const FIT_PADDING = 32
const LEGIBLE_SCALE = 0.6

const clamp = (value: number, lo: number, hi: number) =>
  Math.min(hi, Math.max(lo, value))

export const useGraphViewport = (
  viewport: Ref<HTMLElement | null>,
  content: ComputedRef<{ width: number; height: number }>,
  focus: () => { x: number; y: number } | null = () => null
) => {
  const viewW = ref(0)
  const viewH = ref(0)
  const scale = ref(1)
  const offsetX = ref(0)
  const offsetY = ref(0)
  const isPanning = ref(false)
  const animate = ref(false)
  let framed = false

  let observer: ResizeObserver | null = null
  onMounted(() => {
    observer = new ResizeObserver(([entry]) => {
      if (!entry) return
      viewW.value = entry.contentRect.width
      viewH.value = entry.contentRect.height
    })
    if (viewport.value) observer.observe(viewport.value)
  })
  onBeforeUnmount(() => observer?.disconnect())

  const withAnimation = (run: () => void) => {
    animate.value = true
    run()
    setTimeout(() => {
      animate.value = false
    }, 220)
  }

  const fit = (smooth = true) => {
    const { width, height } = content.value
    if (!width || !height || !viewW.value || !viewH.value) return
    const apply = () => {
      const k = clamp(
        Math.min(
          (viewW.value - FIT_PADDING * 2) / width,
          (viewH.value - FIT_PADDING * 2) / height,
          1
        ),
        MIN_SCALE,
        MAX_SCALE
      )
      scale.value = k
      offsetX.value = (viewW.value - width * k) / 2
      offsetY.value = Math.max(FIT_PADDING, (viewH.value - height * k) / 2)
    }
    if (smooth) withAnimation(apply)
    else apply()
  }

  const frameLegibly = () => {
    const { width, height } = content.value
    const k = Math.min(
      (viewW.value - FIT_PADDING * 2) / width,
      (viewH.value - FIT_PADDING * 2) / height,
      1
    )
    const start = focus()
    if (k >= LEGIBLE_SCALE || !start) {
      fit(false)
      return
    }
    scale.value = LEGIBLE_SCALE
    offsetX.value = viewW.value / 2 - start.x * LEGIBLE_SCALE
    offsetY.value = FIT_PADDING * 2 - start.y * LEGIBLE_SCALE
  }

  const zoomAt = (factor: number, atX: number, atY: number) => {
    const next = clamp(scale.value * factor, MIN_SCALE, MAX_SCALE)
    const ratio = next / scale.value
    offsetX.value = atX - (atX - offsetX.value) * ratio
    offsetY.value = atY - (atY - offsetY.value) * ratio
    scale.value = next
  }

  const zoomBy = (factor: number) =>
    withAnimation(() => zoomAt(factor, viewW.value / 2, viewH.value / 2))

  const reveal = (x: number, y: number, w = 0, h = 0) => {
    const margin = 48
    const left = offsetX.value + (x - w / 2) * scale.value
    const right = offsetX.value + (x + w / 2) * scale.value
    const top = offsetY.value + (y - h / 2) * scale.value
    const bottom = offsetY.value + (y + h / 2) * scale.value
    if (
      left >= margin &&
      right <= viewW.value - margin &&
      top >= margin &&
      bottom <= viewH.value - margin
    ) {
      return
    }
    withAnimation(() => {
      offsetX.value = viewW.value / 2 - x * scale.value
      offsetY.value = viewH.value / 2 - y * scale.value
    })
  }

  let last: { x: number; y: number } | null = null
  let moved = 0

  const onPointerDown = (event: PointerEvent) => {
    if (event.button !== 0 && event.button !== 1) return
    last = { x: event.clientX, y: event.clientY }
    moved = 0
  }

  const onPointerMove = (event: PointerEvent) => {
    if (!last) return
    const dx = event.clientX - last.x
    const dy = event.clientY - last.y
    moved += Math.abs(dx) + Math.abs(dy)
    if (!isPanning.value) {
      if (moved <= 4) return
      isPanning.value = true
      ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
    }
    offsetX.value += dx
    offsetY.value += dy
    last = { x: event.clientX, y: event.clientY }
  }

  const onPointerUp = () => {
    last = null
    isPanning.value = false
  }

  const wasDrag = () => moved > 4

  const onWheel = (event: WheelEvent) => {
    event.preventDefault()
    if (!(event.ctrlKey || event.metaKey)) {
      const unit = event.deltaMode === 1 ? 16 : 1
      offsetX.value -= event.deltaX * unit
      offsetY.value -= event.deltaY * unit
      return
    }
    const box = viewport.value?.getBoundingClientRect()
    const x = event.clientX - (box?.left ?? 0)
    const y = event.clientY - (box?.top ?? 0)
    const strength = Math.min(Math.abs(event.deltaY) / 100, 2)
    zoomAt(
      event.deltaY < 0 ? 1 + 0.12 * strength : 1 / (1 + 0.12 * strength),
      x,
      y
    )
  }

  watch(
    [() => content.value.width > 0, viewW, viewH],
    ([ready]) => {
      if (ready && !framed && viewW.value && viewH.value) {
        framed = true
        frameLegibly()
      }
    },
    { immediate: true }
  )

  const transform = computed(
    () =>
      `translate(${offsetX.value}px, ${offsetY.value}px) scale(${scale.value})`
  )

  return {
    scale,
    isPanning,
    animate,
    transform,
    fit,
    zoomBy,
    reveal,
    wasDrag,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onWheel
  }
}
