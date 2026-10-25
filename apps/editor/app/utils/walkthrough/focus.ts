import { nextTick } from 'vue'

const STEP_INPUT = '[data-wt-step-input]'

const nextFrame = () => new Promise((resolve) => requestAnimationFrame(resolve))

// Segment cards use content-visibility: auto, so cards between here and the
// target only have their estimated height until they render. A single scroll
// landed a revealed segment at the bottom of the outline instead of its top.
export const settleInto = async (
  el: Element,
  block: ScrollLogicalPosition = 'start'
) => {
  for (let i = 0; i < 3; i++) {
    el.scrollIntoView({ block })
    await nextFrame()
  }
}

export const stepInputSelector = (segment: string, index: number) =>
  `[data-wt-step-input="${segment}:${index}"]`

export const focusStep = async (
  segment: string,
  index: number,
  caret: 'start' | 'end' = 'end'
) => {
  await nextTick()
  const el = document.querySelector<HTMLElement>(
    stepInputSelector(segment, index)
  )
  if (!el) return false
  el.focus()
  if (el instanceof HTMLInputElement) {
    const at = caret === 'start' ? 0 : el.value.length
    el.setSelectionRange(at, at)
  }
  const box = el.getBoundingClientRect()
  if (box.top < 0 || box.bottom > window.innerHeight) {
    await settleInto(el, 'center')
  }
  return true
}

export const focusSibling = (from: HTMLElement, step: number) => {
  const all = [...document.querySelectorAll<HTMLElement>(STEP_INPUT)]
  const next = all[all.indexOf(from) + step]
  if (!next) return false
  next.focus()
  if (next instanceof HTMLInputElement) {
    const at = step < 0 ? next.value.length : 0
    next.setSelectionRange(at, at)
  }
  next.scrollIntoView({ block: 'nearest' })
  return true
}

export const isTyping = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null
  return (
    !!target &&
    (target.isContentEditable ||
      ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  )
}

export const isMod = (event: KeyboardEvent) => event.metaKey || event.ctrlKey

export const composing = (event: KeyboardEvent) =>
  event.isComposing || event.keyCode === 229
