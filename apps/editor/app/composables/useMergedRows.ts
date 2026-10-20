import type { EditRefFamily, EditUpstreamRow } from '#shared/types/edit'
import { WRITER_LABELS } from '~/constants/editEntities'
import { LINK_SOURCE_LABELS } from '~/utils/externalLinks'
import {
  isMergedRowHidden,
  mergeRows,
  restoreRemovedRow,
  summarizeMerged,
  toggleSuppression,
  type MergedRow
} from '~/utils/mergedRows'

export const upstreamSourceLabel = (upstream?: EditUpstreamRow) => {
  if (!upstream) {
    return ''
  }
  const key = upstream.sourceKey ?? ''
  if (key === 'curated' || key === 'user') {
    return upstream.machine ? '机翻' : '人工'
  }
  return (
    LINK_SOURCE_LABELS[key] ?? WRITER_LABELS[key] ?? upstream.source ?? '资料源'
  )
}

export const sourceLabelOf = (row: MergedRow) =>
  upstreamSourceLabel(row.upstream)

export const useMergedRows = (fieldKey: MaybeRefOrGetter<string>) => {
  const field = useEditorField(fieldKey)
  const { context, key, value, baseValue, companion, companionKey, disabled } =
    field
  const editor = context.editor
  const refs = context.refs

  const draftList = computed<unknown[]>(() =>
    Array.isArray(value.value) ? value.value : []
  )
  const baseList = computed<unknown[]>(() =>
    Array.isArray(baseValue.value) ? baseValue.value : []
  )
  const draftSuppressed = computed<string[]>(() => {
    const raw = editor.draft.value[companionKey.value]
    return Array.isArray(raw) ? raw.map(String) : []
  })
  const baseSuppressed = computed<string[]>(() => {
    const raw = editor.base.value[companionKey.value]
    return Array.isArray(raw) ? raw.map(String) : []
  })
  const upstream = computed(() => context.upstreamRows.value[key.value] ?? [])

  const identityOf = (row: unknown) => rowIdentity(key.value, row)

  const rows = computed(() =>
    mergeRows({
      draft: draftList.value,
      base: baseList.value,
      upstream: upstream.value,
      suppressed: companion.value ? draftSuppressed.value : [],
      baseSuppressed: companion.value ? baseSuppressed.value : [],
      identityOf
    })
  )
  const summary = computed(() => summarizeMerged(rows.value))

  const setDraft = (next: unknown[]) => {
    value.value = next
  }
  const setSuppressed = (next: string[]) => {
    editor.setValue(companionKey.value, next)
  }

  const patchAt = (index: number, patch: Record<string, unknown>) => {
    setDraft(
      draftList.value.map((row, i) =>
        i === index ? { ...(row as Record<string, unknown>), ...patch } : row
      )
    )
  }
  const setAt = (index: number, next: unknown) => {
    setDraft(draftList.value.map((row, i) => (i === index ? next : row)))
  }
  const removeAt = (index: number) => {
    setDraft(draftList.value.filter((_, i) => i !== index))
  }
  const append = (row: unknown) => {
    setDraft([...draftList.value, row])
  }

  const canHide = (row: MergedRow) =>
    !disabled.value &&
    Boolean(companion.value) &&
    Boolean(row.identity) &&
    row.lane !== 'human'

  const hide = (row: MergedRow) => {
    if (canHide(row) && !isMergedRowHidden(row)) {
      setSuppressed(toggleSuppression(draftSuppressed.value, row.identity!))
    }
  }

  const restore = (row: MergedRow) => {
    if (row.status === 'removed') {
      setDraft(restoreRemovedRow(draftList.value, baseList.value, row))
      return
    }
    if (row.identity && draftSuppressed.value.includes(row.identity)) {
      setSuppressed(toggleSuppression(draftSuppressed.value, row.identity))
    }
  }

  const undo = (row: MergedRow) => {
    if (row.status === 'unhiding' && row.identity) {
      setSuppressed([...draftSuppressed.value, row.identity])
    } else if (row.status === 'hiding') {
      restore(row)
    } else if (row.status === 'removed') {
      restore(row)
    } else if (row.lane === 'human' && row.status === 'added') {
      removeAt(row.index)
    } else if (row.lane === 'human' && row.status === 'changed') {
      setAt(row.index, cloneValue(row.base))
    }
  }

  const ghostLabel = (row: MergedRow) => {
    const parts = suppressedKeyParts(row.identity ?? '')
    if (parts.label) {
      return parts.label
    }
    const names = parts.refs.map(
      (ref) => refs.nameOf(ref.family, ref.id) || `#${ref.id}`
    )
    return names.length ? names.join(' · ') : (row.identity ?? '')
  }

  watch(
    rows,
    (list) => {
      const byFamily = new Map<EditRefFamily, Set<string>>()
      const add = (family: EditRefFamily, id: string) =>
        byFamily.set(family, (byFamily.get(family) ?? new Set()).add(id))
      for (const row of list) {
        if (row.upstream?.ref) {
          add(row.upstream.ref.family, row.upstream.ref.id)
        }
        if (row.upstream?.characterId) {
          add('character', row.upstream.characterId)
        }
        if (row.lane === 'ghost') {
          for (const ref of suppressedKeyParts(row.identity ?? '').refs) {
            add(ref.family, ref.id)
          }
        }
      }
      for (const [family, ids] of byFamily) {
        refs.resolve(family, [...ids])
      }
    },
    { immediate: true }
  )

  return {
    ...field,
    draftList,
    baseList,
    draftSuppressed,
    rows,
    summary,
    identityOf,
    setDraft,
    setSuppressed,
    patchAt,
    setAt,
    removeAt,
    append,
    canHide,
    hide,
    restore,
    undo,
    ghostLabel
  }
}

export type MergedRowsState = ReturnType<typeof useMergedRows>
