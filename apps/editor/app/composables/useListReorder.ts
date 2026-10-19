export const moveItem = <T>(items: T[], from: number, to: number) => {
  if (
    from === to ||
    from < 0 ||
    to < 0 ||
    from >= items.length ||
    to >= items.length
  ) {
    return items
  }
  const next = [...items]
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved as T)
  return next
}

export const useListReorder = <T>(
  items: () => T[],
  update: (next: T[]) => void
) => {
  const dragIndex = ref<number | null>(null)
  const overIndex = ref<number | null>(null)

  const move = (from: number, to: number) => {
    const current = items()
    const next = moveItem(current, from, to)
    if (next !== current) {
      update(next)
    }
  }

  const reset = () => {
    dragIndex.value = null
    overIndex.value = null
  }

  const handleAttrs = (index: number) => ({
    draggable: true,
    onDragstart: (event: DragEvent) => {
      dragIndex.value = index
      if (event.dataTransfer) {
        event.dataTransfer.effectAllowed = 'move'
        event.dataTransfer.setData('text/plain', String(index))
      }
    },
    onDragend: reset
  })

  const dropAttrs = (index: number) => ({
    onDragover: (event: DragEvent) => {
      if (dragIndex.value === null) {
        return
      }
      event.preventDefault()
      overIndex.value = index
    },
    onDrop: (event: DragEvent) => {
      if (dragIndex.value === null) {
        return
      }
      event.preventDefault()
      move(dragIndex.value, index)
      reset()
    }
  })

  const onKeydown = (event: KeyboardEvent, index: number) => {
    if (!event.altKey) {
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      move(index, index - 1)
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      move(index, index + 1)
    }
  }

  return { dragIndex, overIndex, move, handleAttrs, dropAttrs, onKeydown }
}
