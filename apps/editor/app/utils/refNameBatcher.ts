export interface RefNameItem {
  key: string
  family: string
  id: string
  display_name: string
  name_zh?: string | null
}

export const REF_NAMES_PER_REQUEST = 500

export const ZH_FIRST_FAMILIES = new Set(['trait', 'tag', 'role'])

export const zhFromLocalized = (
  localized: Record<string, { value?: string } | undefined> | null | undefined
) =>
  ['zh-Hans', 'zh-Hant', 'zh']
    .map((lang) => localized?.[lang]?.value ?? '')
    .find(Boolean) ?? ''

export const preferredRefLabel = (item: RefNameItem) =>
  (ZH_FIRST_FAMILIES.has(item.family) && item.name_zh) ||
  item.display_name ||
  item.name_zh ||
  ''

export const secondaryRefLabel = (item: RefNameItem) => {
  const label = preferredRefLabel(item)
  return item.display_name && item.display_name !== label
    ? item.display_name
    : ''
}

type Waiter = (item: RefNameItem | null) => void

export const createRefNameBatcher = (
  fetchKeys: (keys: string[]) => Promise<RefNameItem[]>,
  delayMs = 16
) => {
  let pending = new Map<string, Waiter[]>()
  const inflight = new Map<string, Promise<RefNameItem | null>>()
  let timer: ReturnType<typeof setTimeout> | null = null

  const flush = async () => {
    timer = null
    const batch = pending
    pending = new Map()
    const keys = [...batch.keys()]
    for (let i = 0; i < keys.length; i += REF_NAMES_PER_REQUEST) {
      const chunk = keys.slice(i, i + REF_NAMES_PER_REQUEST)
      const items = await fetchKeys(chunk).catch(() => [])
      const byKey = new Map(items.map((item) => [item.key, item]))
      for (const key of chunk) {
        inflight.delete(key)
        for (const done of batch.get(key) ?? []) {
          done(byKey.get(key) ?? null)
        }
      }
    }
  }

  const one = (key: string) => {
    const running = inflight.get(key)
    if (running) {
      return running
    }
    const promise = new Promise<RefNameItem | null>((resolve) => {
      pending.set(key, [...(pending.get(key) ?? []), resolve])
    })
    inflight.set(key, promise)
    timer ??= setTimeout(flush, delayMs)
    return promise
  }

  const request = async (keys: string[]) => {
    const unique = [...new Set(keys)]
    const items = await Promise.all(unique.map(one))
    return new Map(unique.map((key, i) => [key, items[i] ?? null]))
  }

  return { request }
}

let shared: ReturnType<typeof createRefNameBatcher> | null = null

export const useRefNameBatcher = () => {
  const api = useEditorApi()
  shared ??= createRefNameBatcher((keys) =>
    api<{ items?: RefNameItem[] }>('/v2/catalog/names', {
      query: { keys: keys.join(','), nsfw: true }
    }).then((list) => list.items ?? [])
  )
  return shared
}
