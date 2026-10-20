import type { CatalogSearchList } from '#shared/types/catalog'
import type { EditRefFamily } from '#shared/types/edit'

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
}

const IDS_PER_REQUEST = 100

interface RoleList {
  items?: {
    id: string
    key: string
    display_name: string
    deprecated?: boolean
  }[]
  next_cursor?: string | null
}

export const useRefNames = () => {
  const api = useEditorApi()
  const names = useState<Record<string, string>>('edit-ref-names', () => ({}))
  const roles = useState<RefOption[] | null>('edit-ref-roles', () => null)
  const roleIdByKey = useState<Record<string, string>>(
    'edit-ref-role-keys',
    () => ({})
  )

  const keyOf = (family: EditRefFamily, id: string | number) =>
    `${family}:${id}`

  const remember = (family: EditRefFamily, id: string, label: string) => {
    if (label) {
      names.value = { ...names.value, [keyOf(family, id)]: label }
    }
  }

  const loadRoles = async () => {
    if (roles.value) {
      return roles.value
    }
    const all: RefOption[] = []
    const keys: Record<string, string> = {}
    let cursor: string | null = null
    for (let page = 0; page < 10; page++) {
      const list: RoleList | null = await api<RoleList>('/v2/catalog/roles', {
        query: { limit: 100, ...(cursor ? { cursor } : {}) }
      }).catch(() => null)
      for (const role of list?.items ?? []) {
        keys[role.key] = role.id
        all.push({
          value: role.id,
          label: role.display_name,
          hint: role.deprecated ? '已停用' : undefined
        })
      }
      cursor = list?.next_cursor ?? null
      if (!cursor) {
        break
      }
    }
    roles.value = all
    roleIdByKey.value = keys
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
    const missing = [
      ...new Set(
        ids
          .map(String)
          .filter((id) => /^\d+$/.test(id) && !names.value[keyOf(family, id)])
      )
    ]
    for (let i = 0; i < missing.length; i += IDS_PER_REQUEST) {
      const chunk = missing.slice(i, i + IDS_PER_REQUEST)
      const list = await api<{
        items?: { id: string; display_name?: string; title?: string }[]
      }>(`/v2/catalog/${REF_COLLECTION[family]}`, {
        query: { ids: chunk.join(','), nsfw: true, limit: chunk.length }
      }).catch(() => null)
      const found = Object.fromEntries(
        (list?.items ?? []).map((item) => [
          keyOf(family, item.id),
          item.display_name ?? item.title ?? ''
        ])
      )
      names.value = { ...names.value, ...found }
    }
  }

  const nameOf = (family: EditRefFamily | undefined, id: unknown) => {
    if (id === null || id === undefined || id === '') {
      return ''
    }
    return family ? (names.value[keyOf(family, String(id))] ?? '') : ''
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
      remember(family, hit.id, hit.display_name)
      return {
        value: hit.id,
        label: hit.display_name,
        hint: [hit.latin, `#${hit.id}`].filter(Boolean).join(' · ')
      }
    })
  }

  return { names, roleIdByKey, nameOf, resolve, search, remember, loadRoles }
}
