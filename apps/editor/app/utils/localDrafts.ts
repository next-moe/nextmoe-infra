import type { LocalDraft } from '#shared/types/activity'
import { ENTITY_FAMILY } from '~/constants/catalog'

const DRAFT_KEY_PATTERN = /^nextmoe-editor:draft:([a-z_]+):(.+)$/

type DraftStorage = Pick<Storage, 'length' | 'key' | 'getItem'>

export const parseLocalDraft = (
  key: string,
  raw: string | null
): LocalDraft | null => {
  const match = key.match(DRAFT_KEY_PATTERN)
  if (!match || !raw) {
    return null
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object') {
    return null
  }
  const { savedAt, values } = parsed as {
    savedAt?: unknown
    values?: unknown
  }
  if (
    typeof savedAt !== 'string' ||
    Number.isNaN(Date.parse(savedAt)) ||
    !values ||
    typeof values !== 'object' ||
    Array.isArray(values)
  ) {
    return null
  }
  const fieldKeys = Object.keys(values)
  if (fieldKeys.length === 0) {
    return null
  }
  return {
    key,
    family: match[1]!,
    entityId: match[2]!,
    savedAt,
    fieldKeys
  }
}

export const readLocalDrafts = (storage: DraftStorage): LocalDraft[] => {
  const drafts: LocalDraft[] = []
  try {
    for (let index = 0; index < storage.length; index++) {
      const key = storage.key(index)
      if (!key) {
        continue
      }
      const draft = parseLocalDraft(key, storage.getItem(key))
      if (draft) {
        drafts.push(draft)
      }
    }
  } catch {
    return drafts
  }
  return drafts.sort((a, b) => Date.parse(b.savedAt) - Date.parse(a.savedAt))
}

export const draftEditPath = (
  draft: Pick<LocalDraft, 'family' | 'entityId'>
) => {
  const family = ENTITY_FAMILY[draft.family]
  return family ? `${family.path}/${draft.entityId}/edit` : null
}
