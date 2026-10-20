import type { ActivityList, ProposalNotice } from '#shared/types/activity'
import type { CatalogProposal } from '#shared/types/catalog'

const POLL_MS = 5 * 60_000
const WINDOW_LIMIT = 50

const readSeenAt = () => {
  try {
    return localStorage.getItem(NOTICE_SEEN_KEY)
  } catch {
    return null
  }
}

const writeSeenAt = (iso: string) => {
  try {
    localStorage.setItem(NOTICE_SEEN_KEY, iso)
  } catch {
    // Without storage every visit starts from "now"; nothing else breaks.
  }
}

export const useProposalNotices = () => {
  const api = useEditorApi()
  const notices = useState<ProposalNotice[]>('editor-notices', () => [])
  const seenAt = useState<string | null>('editor-notices-seen', () => null)

  const refresh = async () => {
    if (seenAt.value === null) {
      seenAt.value = readSeenAt()
      if (seenAt.value === null) {
        seenAt.value = new Date().toISOString()
        writeSeenAt(seenAt.value)
      }
    }
    const list = await api<ActivityList<CatalogProposal>>('/v2/me/proposals', {
      query: {
        limit: WINDOW_LIMIT,
        sort: 'updated_desc',
        ...(seenAt.value ? { updated_since: seenAt.value } : {})
      }
    }).catch(() => null)
    if (list) {
      notices.value = proposalNotices(list.items ?? [], seenAt.value)
    }
  }

  const markAllSeen = () => {
    const latest = latestNoticeTime(notices.value)
    const next = latest ?? new Date().toISOString()
    seenAt.value = next
    writeSeenAt(next)
    notices.value = []
  }

  const startPolling = () => {
    const timer = setInterval(refresh, POLL_MS)
    const onFocus = () => {
      if (document.visibilityState === 'visible') {
        refresh()
      }
    }
    document.addEventListener('visibilitychange', onFocus)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onFocus)
    }
  }

  return { notices, refresh, markAllSeen, startPolling }
}
