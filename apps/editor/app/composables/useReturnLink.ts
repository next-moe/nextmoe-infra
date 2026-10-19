import { RETURN_LINK_STORAGE_KEY, RETURN_SITES } from '~/constants/returnSites'
import type { ReturnLink } from '~/utils/returnLink'

const readStored = (): ReturnLink | null => {
  try {
    const raw = sessionStorage.getItem(RETURN_LINK_STORAGE_KEY)
    if (!raw) {
      return null
    }
    const parsed = JSON.parse(raw) as { from?: string; url?: string }
    return resolveReturnLink(parsed.from, parsed.url, RETURN_SITES)
  } catch {
    return null
  }
}

export const useReturnLink = () => {
  const link = useState<ReturnLink | null>('editor-return-link', () => null)
  const route = useRoute()

  const capture = () => {
    const fromQuery = resolveReturnLink(
      route.query.from,
      route.query.return,
      RETURN_SITES
    )
    if (fromQuery) {
      link.value = fromQuery
      try {
        sessionStorage.setItem(
          RETURN_LINK_STORAGE_KEY,
          JSON.stringify({ from: route.query.from, url: fromQuery.url })
        )
      } catch {
        // Without storage the link still shows for this page view.
      }
      return
    }
    link.value = readStored()
  }

  return { link, capture }
}
