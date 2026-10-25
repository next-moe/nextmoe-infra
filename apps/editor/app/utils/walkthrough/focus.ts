import { nextTick } from 'vue'

const STEP_INPUT = '[data-wt-step-input]'

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
  el.scrollIntoView({ block: 'nearest' })
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
