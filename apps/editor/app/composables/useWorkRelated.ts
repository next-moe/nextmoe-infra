import type { CatalogEditContextRow, CatalogWork } from '#shared/types/catalog'
import type { CatalogSchemaMember } from '#shared/types/edit'
import type { EntityWorkspace } from './useEntityWorkspace'
import { vocabLabel } from '~/constants/vocabLabels'

export interface RelatedPicture {
  url: string
  thumbhash: string | null
}

export interface RelatedCharacter {
  id: string
  name: string
  latin: string
  image: RelatedPicture | null
  voices: { id: string; name: string; personId: string | null }[]
}

export interface RelatedCompany {
  id: string
  name: string
  kind: string
  logo: RelatedPicture | null
  workCount: number | null
}

export interface RelatedWorkCard {
  id: string
  name: string
  medium: string
  cover: RelatedPicture | null
  releaseDate: string | null
}

export interface RelatedSeries {
  id: string
  name: string
  source: string
  memberCount: number | null
}

export interface RelatedRelease {
  id: string
  title: string
  kind: string
  date: string | null
  lang: string
  platforms: string[]
  languages: string[]
  freeware: boolean
  official: boolean
  minAge: number | null
}

interface RelatedDisplay {
  loaded: boolean
  characters: Record<string, RelatedCharacter>
  companies: Record<string, RelatedCompany>
  works: Record<string, RelatedWorkCard>
  series: Record<string, RelatedSeries>
  engines: Record<string, { id: string; name: string }>
  releases: RelatedRelease[]
  creditPersons: Record<string, string | null>
}

const RELATED_INCLUDE =
  'companies,engines,series,relations,platforms,credits,characters'

const emptyDisplay = (): RelatedDisplay => ({
  loaded: false,
  characters: {},
  companies: {},
  works: {},
  series: {},
  engines: {},
  releases: [],
  creditPersons: {}
})

type Loose = Record<string, unknown>

const str = (value: unknown) =>
  value === null || value === undefined ? '' : String(value)

const pictureOf = (value: unknown): RelatedPicture | null => {
  const record = (value ?? {}) as Loose
  return typeof record.url === 'string' && record.url
    ? {
        url: record.url,
        thumbhash:
          typeof record.thumbhash === 'string' ? record.thumbhash : null
      }
    : null
}

const characterOf = (raw: Loose): RelatedCharacter => ({
  id: str(raw.id),
  name: str(raw.display_name),
  latin: str(raw.latin),
  image: pictureOf(raw.image),
  voices: ((raw.voices as Loose[] | undefined) ?? []).map((voice) => ({
    id: str(voice.id),
    name: str(voice.display_name),
    personId: voice.person_id ? str(voice.person_id) : null
  }))
})

const companyOf = (raw: Loose): RelatedCompany => ({
  id: str(raw.id),
  name: str(raw.display_name),
  kind: str(raw.company_kind),
  logo: pictureOf(raw.logo),
  workCount: typeof raw.work_count === 'number' ? raw.work_count : null
})

const workCardOf = (raw: Loose): RelatedWorkCard => ({
  id: str(raw.id),
  name: str(raw.display_name),
  medium: str(raw.medium),
  cover: pictureOf(raw.cover),
  releaseDate: raw.release_date ? str(raw.release_date) : null
})

const releaseOf = (raw: Loose): RelatedRelease => ({
  id: str(raw.id),
  title: str(raw.title),
  kind: str(raw.release_kind),
  date: raw.date ? str(raw.date) : null,
  lang: str(raw.lang),
  platforms: ((raw.platforms as string[] | undefined) ?? []).map(String),
  languages: ((raw.languages as string[] | undefined) ?? []).map(String),
  freeware: raw.freeware === true,
  official: raw.official !== false,
  minAge: typeof raw.min_age === 'number' ? raw.min_age : null
})

const byId = <T extends { id: string }>(items: T[]) =>
  Object.fromEntries(items.map((item) => [item.id, item]))

const inflight = new Map<string, Promise<void>>()

export const useWorkRelated = (workId: MaybeRefOrGetter<string>) => {
  const api = useEditorApi()
  const id = computed(() => toValue(workId))
  const store = useState<Record<string, RelatedDisplay>>(
    'workspace-related-display',
    () => ({})
  )

  const display = computed(() => store.value[id.value] ?? emptyDisplay())

  const patch = (next: Partial<RelatedDisplay>) => {
    const current = store.value[id.value] ?? emptyDisplay()
    store.value = { ...store.value, [id.value]: { ...current, ...next } }
  }

  const mergeInto = <K extends keyof RelatedDisplay>(
    key: K,
    items: RelatedDisplay[K]
  ) => {
    if (!Object.keys(items as object).length) {
      return
    }
    const current = (store.value[id.value] ?? emptyDisplay())[key]
    patch({
      [key]: Array.isArray(current)
        ? items
        : { ...(current as object), ...(items as object) }
    } as Partial<RelatedDisplay>)
  }

  const fetchWork = async () => {
    const work = await api<CatalogWork & Loose>(
      `/v2/catalog/works/${id.value}`,
      {
        query: { nsfw: true, view: 'full', include: RELATED_INCLUDE }
      }
    ).catch(() => null)
    if (!work) {
      patch({ loaded: true })
      return
    }
    const raw = work as Loose
    const characters = ((raw.characters as Loose[] | undefined) ?? []).map(
      characterOf
    )
    const creditPersons: Record<string, string | null> = {}
    for (const character of characters) {
      for (const voice of character.voices) {
        creditPersons[voice.id] = voice.personId
      }
    }
    patch({
      loaded: true,
      characters: byId(characters),
      companies: byId(
        ((raw.companies as Loose[] | undefined) ?? []).map(companyOf)
      ),
      works: byId(
        ((raw.relations as Loose[] | undefined) ?? []).map((relation) =>
          workCardOf((relation.work ?? {}) as Loose)
        )
      ),
      series: byId(
        ((raw.series as Loose[] | undefined) ?? []).map((series) => ({
          id: str(series.id),
          name: str(series.display_name),
          source: str(series.source),
          memberCount:
            typeof series.member_count === 'number' ? series.member_count : null
        }))
      ),
      engines: byId(
        ((raw.engines as Loose[] | undefined) ?? []).map((engine) => ({
          id: str(engine.id),
          name: str(engine.display_name)
        }))
      ),
      releases: ((raw.releases as Loose[] | undefined) ?? []).map(releaseOf),
      creditPersons: {
        ...(store.value[id.value]?.creditPersons ?? {}),
        ...creditPersons
      }
    })
  }

  const load = (force = false) => {
    const key = id.value
    if (!force && (store.value[key]?.loaded || inflight.has(key))) {
      return inflight.get(key) ?? Promise.resolve()
    }
    const job = fetchWork().finally(() => inflight.delete(key))
    inflight.set(key, job)
    return job
  }

  const missing = (have: Record<string, unknown>, ids: unknown[]) => [
    ...new Set(
      ids.map(str).filter((one) => /^\d+$/.test(one) && !(one in have))
    )
  ]

  const ensureCharacters = async (ids: unknown[]) => {
    const want = missing(display.value.characters, ids)
    if (!want.length) {
      return
    }
    const found = await Promise.all(
      want.slice(0, 20).map((one) =>
        api<Loose>(`/v2/catalog/characters/${one}`, {
          query: { nsfw: true, view: 'full' }
        })
          .then(characterOf)
          .catch(() => null)
      )
    )
    mergeInto(
      'characters',
      byId(found.filter((item): item is RelatedCharacter => item !== null))
    )
  }

  const ensureCompanies = async (ids: unknown[]) => {
    const want = missing(display.value.companies, ids)
    if (!want.length) {
      return
    }
    const found = await Promise.all(
      want.slice(0, 20).map((one) =>
        api<Loose>(`/v2/catalog/companies/${one}`, {
          query: { nsfw: true, view: 'full' }
        })
          .then(companyOf)
          .catch(() => null)
      )
    )
    mergeInto(
      'companies',
      byId(found.filter((item): item is RelatedCompany => item !== null))
    )
  }

  const ensureWorks = async (ids: unknown[]) => {
    const want = missing(display.value.works, ids)
    if (!want.length) {
      return
    }
    const list = await api<{ items?: Loose[] }>('/v2/catalog/works', {
      query: { ids: want.join(','), nsfw: true, limit: want.length }
    }).catch(() => null)
    mergeInto('works', byId((list?.items ?? []).map(workCardOf)))
  }

  const ensureCreditPersons = async (ids: unknown[]) => {
    const want = missing(display.value.creditPersons, ids)
    for (let i = 0; i < want.length; i += 100) {
      const chunk = want.slice(i, i + 100)
      const list = await api<{ items?: Loose[] }>('/v2/catalog/credit-names', {
        query: { ids: chunk.join(','), nsfw: true, limit: chunk.length }
      }).catch(() => null)
      mergeInto(
        'creditPersons',
        Object.fromEntries(
          chunk.map((one) => {
            const item = (list?.items ?? []).find(
              (entry) => str(entry.id) === one
            )
            return [one, item?.person_id ? str(item.person_id) : null]
          })
        )
      )
    }
  }

  return {
    display,
    load,
    ensureCharacters,
    ensureCompanies,
    ensureWorks,
    ensureCreditPersons
  }
}

export const useRelatedField = (fieldKey: string) => {
  const field = useEditorField(fieldKey)
  const session = useWorkspaceSession()
  const { byName } = useVocabularies()

  const contextRows = computed<CatalogEditContextRow[]>(
    () =>
      session.root.workspace.editContext.context.value?.fields?.find(
        (entry) => entry.key === fieldKey
      )?.rows ?? []
  )

  const list = computed<unknown[]>(() =>
    Array.isArray(field.value.value) ? (field.value.value as unknown[]) : []
  )
  const baseList = computed<unknown[]>(() =>
    Array.isArray(field.baseValue.value)
      ? (field.baseValue.value as unknown[])
      : []
  )

  const memberOf = (key: string) =>
    field.field.value?.element?.members?.find(
      (member) => member.key === key
    ) as CatalogSchemaMember | undefined

  const memberOptions = (key: string, labels?: Record<string, string>) => {
    const member = memberOf(key)
    const slot = member ? memberVocabSlot(member) : null
    const vocab = slot ? byName.value[slot.vocabulary] : undefined
    if (!slot || !vocab) {
      return []
    }
    return vocabWireValues(slot, vocab).map(({ wire, token }) => ({
      value: wire as string | number,
      token,
      label:
        labels?.[token] ||
        vocabLabel(
          slot.vocabulary,
          token,
          vocab.values.find((entry) => entry.value === token)?.display_name
        )
    }))
  }

  const tokenOf = (key: string, wire: unknown) => {
    const member = memberOf(key)
    const slot = member ? memberVocabSlot(member) : null
    return slot ? vocabTokenOf(slot, byName.value[slot.vocabulary], wire) : ''
  }

  const wireBase = (key: string) => {
    const member = memberOf(key)
    return (member ? memberVocabSlot(member)?.base : 0) ?? 0
  }

  const setList = (next: unknown[]) => {
    field.value.value = next
  }
  const setAt = (index: number, patchValue: Record<string, unknown>) => {
    setList(
      list.value.map((row, i) =>
        i === index
          ? { ...(row as Record<string, unknown>), ...patchValue }
          : row
      )
    )
  }
  const removeAt = (index: number) => {
    setList(list.value.filter((_, i) => i !== index))
  }
  const append = (row: unknown) => {
    setList([...list.value, row])
    return list.value.length - 1
  }

  return {
    ...field,
    contextRows,
    list,
    baseList,
    memberOf,
    memberOptions,
    tokenOf,
    wireBase,
    setList,
    setAt,
    removeAt,
    append
  }
}

export type RelatedField = ReturnType<typeof useRelatedField>

export const useWorkRelatedCounts = (workspace: EntityWorkspace) => {
  const related = useWorkRelated(workspace.id)
  return computed<Record<string, number | null>>(() => {
    if (workspace.family !== 'work' || !workspace.loaded.value) {
      return {} as Record<string, number | null>
    }
    const draft = workspace.editor.draft.value
    const base = workspace.editor.base.value
    const contextOf = (key: string) =>
      workspace.editContext.context.value?.fields?.find(
        (entry) => entry.key === key
      )?.rows ?? null
    const listOf = (values: Record<string, unknown>, key: string) =>
      Array.isArray(values[key]) ? (values[key] as unknown[]) : []
    const suppressedOf = (key: string) =>
      listOf(draft, `${key}.suppressed`).map(String)
    const rows = (key: string) =>
      visibleRows(
        mergeRelatedRows({
          fieldKey: key,
          human: listOf(draft, key),
          base: listOf(base, key),
          context: contextOf(key),
          suppressed: suppressedOf(key)
        })
      )
    const ids = (key: string) =>
      mergeIdRows({
        human: listOf(draft, key),
        base: listOf(base, key),
        context: contextOf(key),
        suppressed: suppressedOf(key)
      }).filter((row) => row.state !== 'hidden').length
    const rosterKey = 'catalog.work.roster'
    const hiddenRoster = new Set(suppressedOf(rosterKey))
    const rosterIdentity = workspace.editContext.rosterIdentity.value
    const roster = listOf(draft, rosterKey).filter((row) => {
      const identity =
        rosterIdentity[String((row as Record<string, unknown>).character_id)]
      return !identity || !hiddenRoster.has(identity)
    }).length
    return {
      roster,
      credits: rows('catalog.work.credits').length,
      companies: new Set(
        rows('catalog.work.labels').map((row) => String(row.value.label_id))
      ).size,
      engines: ids('catalog.work.engine_ids'),
      series: ids('catalog.work.series_ids'),
      relations: rows('catalog.work.relations').length,
      releases: related.display.value.loaded
        ? related.display.value.releases.length
        : null
    }
  })
}
