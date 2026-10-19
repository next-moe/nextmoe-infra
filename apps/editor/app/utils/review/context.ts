import type { CatalogSchemaField } from '#shared/types/catalog'
import {
  ATTRIBUTION_ROLE_LABELS,
  CONTENT_RATING,
  LANGUAGE_LABELS,
  ROSTER_ROLE_LABELS,
  SPOILER_LABELS,
  TITLE_KIND_LABELS
} from '~/constants/catalog'
import { useEditorApi } from '~/composables/useEditorApi'
import {
  refCollectionOf,
  type RefCollection,
  type VocabularyInfo,
  type VocabularyValue
} from './describe'
import { asList } from './listDiff'

const LABEL_OVERRIDES: Record<string, Record<string, string>> = {
  content_rating: Object.fromEntries(
    Object.entries(CONTENT_RATING).map(([k, v]) => [k, v.label])
  ),
  olang: LANGUAGE_LABELS,
  intro_lang: LANGUAGE_LABELS,
  release_lang: LANGUAGE_LABELS,
  title_kind: TITLE_KIND_LABELS,
  attribution_role: ATTRIBUTION_ROLE_LABELS,
  roster_role: ROSTER_ROLE_LABELS,
  spoiler: { ...SPOILER_LABELS, none: '无' }
}

export interface DiffContext {
  vocabulary: (name: string) => VocabularyInfo | undefined
  refName: (collection: RefCollection, id: unknown) => string
}

const vocabularyNames = (fields: readonly CatalogSchemaField[]) => {
  const names = new Set<string>()
  for (const field of fields) {
    if (field.vocabulary) {
      names.add(field.vocabulary)
    }
    for (const member of field.element?.members ?? []) {
      if (member.vocabulary) {
        names.add(member.vocabulary)
      }
    }
  }
  return names
}

export const collectRefs = (
  field: CatalogSchemaField,
  value: unknown,
  into: Map<RefCollection, Set<string>>
) => {
  const add = (collection: RefCollection | null, id: unknown) => {
    if (!collection || id === null || id === undefined || id === '') {
      return
    }
    const set = into.get(collection) ?? new Set<string>()
    set.add(String(id))
    into.set(collection, set)
  }
  if (field.field_type === 'ref') {
    add(refCollectionOf(field.key), value)
    return
  }
  if (field.field_type !== 'list' || !field.element) {
    return
  }
  for (const item of asList(value)) {
    if (field.element.type === 'ref') {
      add(refCollectionOf(field.key), item)
    } else if (
      field.element.type === 'object' &&
      item &&
      typeof item === 'object'
    ) {
      for (const member of field.element.members) {
        if (member.type === 'ref') {
          add(
            refCollectionOf(field.key, member.key),
            (item as Record<string, unknown>)[member.key]
          )
        }
      }
    }
  }
}

export const useDiffContext = () => {
  const api = useEditorApi()
  const vocabularies = ref<Record<string, VocabularyInfo>>({})
  const names = ref<Record<string, string>>({})

  const loadVocabularies = async (fields: readonly CatalogSchemaField[]) => {
    const missing = [...vocabularyNames(fields)].filter(
      (name) => !vocabularies.value[name]
    )
    const loaded = await Promise.all(
      missing.map(async (name) => {
        const vocabulary = await api<{ values?: VocabularyValue[] }>(
          `/v2/vocabularies/${encodeURIComponent(name)}`
        ).catch(() => null)
        return [
          name,
          { values: vocabulary?.values ?? [], labels: LABEL_OVERRIDES[name] }
        ] as const
      })
    )
    vocabularies.value = {
      ...vocabularies.value,
      ...Object.fromEntries(loaded)
    }
  }

  const resolveRefs = async (refs: Map<RefCollection, Set<string>>) => {
    const jobs: Promise<[string, string][]>[] = []
    for (const [collection, ids] of refs) {
      const pending = [...ids].filter(
        (id) => !names.value[`${collection}:${id}`]
      )
      for (let i = 0; i < pending.length; i += 100) {
        const chunk = pending.slice(i, i + 100)
        jobs.push(
          api<{ items?: { id: string; display_name?: string }[] }>(
            `/v2/catalog/${collection}`,
            { query: { ids: chunk.join(','), nsfw: true } }
          )
            .then((list) =>
              (list.items ?? []).map(
                (item) =>
                  [`${collection}:${item.id}`, item.display_name ?? ''] as [
                    string,
                    string
                  ]
              )
            )
            .catch(() => [])
        )
      }
    }
    const found = (await Promise.all(jobs)).flat()
    names.value = { ...names.value, ...Object.fromEntries(found) }
  }

  const prepare = async (
    fields: readonly CatalogSchemaField[],
    values: { key: string; value: unknown }[]
  ) => {
    const byKey = new Map(fields.map((field) => [field.key, field]))
    const refs = new Map<RefCollection, Set<string>>()
    for (const { key, value } of values) {
      const field = byKey.get(key)
      if (field) {
        collectRefs(field, value, refs)
      }
    }
    await Promise.all([loadVocabularies(fields), resolveRefs(refs)])
  }

  const context: DiffContext = {
    vocabulary: (name) => vocabularies.value[name],
    refName: (collection, id) =>
      names.value[`${collection}:${id}`] || `#${String(id)}`
  }

  return { prepare, context }
}
