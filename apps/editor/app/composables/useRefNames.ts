import type { CatalogSearchList } from '#shared/types/catalog'
import type { EditRefFamily } from '#shared/types/edit'
import type { NameSource } from '#shared/types/preferences'
import { roleOptionsOf, type RoleListItem } from '~/utils/roleOptions'

export const REF_COLLECTION: Record<EditRefFamily, string> = {
  work: 'works',
  tag: 'tags',
  company: 'companies',
  engine: 'engines',
  series: 'series',
  character: 'characters',
  credit_name: 'credit-names',
  person: 'persons',
  role: 'roles',
  trait: 'traits',
  release: 'releases'
}

export const REF_SEARCHABLE = new Set<EditRefFamily>([
  'work',
  'tag',
  'company',
  'engine',
  'series',
  'character',
  'credit_name',
  'trait'
])

export const REF_FAMILY_LABEL: Record<EditRefFamily, string> = {
  work: '作品',
  tag: '标签',
  company: '厂商',
  engine: '引擎',
  series: '系列',
  character: '角色',
  credit_name: '署名',
  person: '人物',
  role: '职位',
  trait: '特质',
  release: '发行'
}

export interface RefOption {
  value: string
  label: string
  hint?: string
  deprecated?: boolean
}

interface RoleList {
  items?: RoleListItem[]
  next_cursor?: string | null
}

const ROLES_CACHE_KEY = 'nextmoe-editor:roles:v2'
const ROLES_CACHE_MAX_AGE_MS = 6 * 60 * 60 * 1000

let rolesRequest: Promise<RoleList['items']> | null = null

export const useRefNames = () => {
  const api = useEditorApi()
  const batcher = useRefNameBatcher()
  const preferences = useEditorPreferences()
  const names = useState<Record<string, string>>('edit-ref-names', () => ({}))
  const sources = useState<Record<string, NameSource>>(
    'edit-ref-sources',
    () => ({})
  )
  const roles = useState<RefOption[] | null>('edit-ref-roles', () => null)
  const roleIdByKey = useState<Record<string, string>>(
    'edit-ref-role-keys',
    () => ({})
  )

  const keyOf = (family: EditRefFamily, id: string | number) =>
    `${family}:${id}`

  const remember = (
    family: EditRefFamily,
    id: string,
    label: string | NameSource
  ) => {
    if (typeof label !== 'string') {
      sources.value = { ...sources.value, [keyOf(family, id)]: label }
    } else if (label) {
      names.value = { ...names.value, [keyOf(family, id)]: label }
    }
  }

  const fetchRoles = async () => {
    const cached = readLocalCache<RoleList['items']>(
      ROLES_CACHE_KEY,
      ROLES_CACHE_MAX_AGE_MS
    )
    if (cached?.length) {
      return cached
    }
    const items: NonNullable<RoleList['items']> = []
    let cursor: string | null = null
    for (let page = 0; page < 10; page++) {
      const list: RoleList | null = await api<RoleList>('/v2/catalog/roles', {
        query: { limit: 100, ...(cursor ? { cursor } : {}) }
      }).catch(() => null)
      items.push(...(list?.items ?? []))
      cursor = list?.next_cursor ?? null
      if (!cursor) {
        break
      }
    }
    if (items.length) {
      writeLocalCache(ROLES_CACHE_KEY, items)
    }
    return items
  }

  const loadRoles = async () => {
    if (roles.value) {
      return roles.value
    }
    rolesRequest ??= fetchRoles().finally(() => {
      rolesRequest = null
    })
    const items = (await rolesRequest) ?? []
    if (roles.value) {
      return roles.value
    }
    const all: RefOption[] = roleOptionsOf(items)
    roles.value = all
    roleIdByKey.value = Object.fromEntries(
      items.map((role) => [role.key, role.id])
    )
    names.value = {
      ...names.value,
      ...Object.fromEntries(all.map((r) => [keyOf('role', r.value), r.label]))
    }
    return all
  }

  const resolve = async (family: EditRefFamily, ids: (string | number)[]) => {
    if (family === 'role') {
      await loadRoles()
      return
    }
    const keys = [
      ...new Set(
        ids
          .map(String)
          .filter(
            (id) =>
              /^\d+$/.test(id) &&
              !sources.value[keyOf(family, id)] &&
              !names.value[keyOf(family, id)]
          )
          .map((id) => keyOf(family, id))
      )
    ]
    if (!keys.length) {
      return
    }
    const found = await batcher.request(keys)
    sources.value = {
      ...sources.value,
      ...Object.fromEntries(
        [...found].filter(([, item]) => item).map(([key, item]) => [key, item!])
      )
    }
  }

  const sourceOf = (family: EditRefFamily | undefined, id: unknown) =>
    family && id !== null && id !== undefined && id !== ''
      ? sources.value[keyOf(family, String(id))]
      : undefined

  const hintOf = (family: EditRefFamily | undefined, id: unknown) =>
    preferences.otherNameOf(sourceOf(family, id))

  const nameOf = (family: EditRefFamily | undefined, id: unknown) => {
    if (!family || id === null || id === undefined || id === '') {
      return ''
    }
    return (
      preferences.nameOf(sourceOf(family, id)) ||
      names.value[keyOf(family, String(id))] ||
      ''
    )
  }

  const searchListFace = async (
    family: 'person' | 'release',
    q: string
  ): Promise<RefOption[]> => {
    const list = await api<{
      items?: {
        id: string
        display_name?: string
        title?: string
        date?: string | null
      }[]
    }>(`/v2/catalog/${family === 'person' ? 'persons' : 'releases'}`, {
      query: { q, limit: 12, nsfw: true }
    }).catch(() => null)
    return (list?.items ?? []).map((item) => {
      const label = item.display_name ?? item.title ?? `#${item.id}`
      remember(family, item.id, label)
      return {
        value: item.id,
        label,
        hint: [item.date, `#${item.id}`].filter(Boolean).join(' · ')
      }
    })
  }

  const search = async (
    family: EditRefFamily,
    query: string
  ): Promise<RefOption[]> => {
    const q = query.trim()
    if (family === 'role') {
      const all = await loadRoles()
      return q ? all.filter((r) => r.label.includes(q)) : all
    }
    if (/^\d+$/.test(q)) {
      await resolve(family, [q])
      const label = nameOf(family, q)
      if (label) {
        return [{ value: q, label, hint: `#${q}` }]
      }
    }
    if (q && (family === 'person' || family === 'release')) {
      return searchListFace(family, q)
    }
    if (!q || !REF_SEARCHABLE.has(family)) {
      return []
    }
    const list = await api<CatalogSearchList>('/v2/catalog/search', {
      query: { q, object: family, limit: 12, nsfw: true }
    }).catch(() => null)
    return (list?.items ?? []).map((hit) => {
      const source = {
        display_name: hit.display_name,
        localized: hit.localized
      }
      remember(family, hit.id, source)
      return {
        value: hit.id,
        label: preferences.nameOf(source) || hit.display_name,
        hint: [preferences.otherNameOf(source) || hit.latin, `#${hit.id}`]
          .filter(Boolean)
          .join(' · ')
      }
    })
  }

  return {
    names,
    roleIdByKey,
    nameOf,
    hintOf,
    resolve,
    search,
    remember,
    loadRoles
  }
}
