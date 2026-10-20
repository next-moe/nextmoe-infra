import type { ProposalNotice } from '#shared/types/activity'
import type { CatalogProposal } from '#shared/types/catalog'

export const NOTICE_SEEN_KEY = 'nextmoe-editor:notices:seen-at'

const timeOf = (iso: string | null | undefined) => {
  if (!iso) {
    return Number.NaN
  }
  return Date.parse(iso)
}

const isAfter = (iso: string | null | undefined, seen: number) => {
  const at = timeOf(iso)
  return !Number.isNaN(at) && at > seen
}

export const proposalNotices = (
  proposals: CatalogProposal[],
  seenAt: string | null
): ProposalNotice[] => {
  const seen = seenAt ? timeOf(seenAt) : Number.NEGATIVE_INFINITY
  const floor = Number.isNaN(seen) ? Number.NEGATIVE_INFINITY : seen
  const notices: ProposalNotice[] = []
  for (const proposal of proposals) {
    if (
      (proposal.state === 'merged' || proposal.state === 'declined') &&
      proposal.decided_at &&
      isAfter(proposal.decided_at, floor)
    ) {
      notices.push({ proposal, kind: proposal.state, at: proposal.decided_at })
      continue
    }
    if (
      proposal.state === 'open' &&
      isAfter(proposal.updated_at, floor) &&
      timeOf(proposal.updated_at) > timeOf(proposal.created_at)
    ) {
      notices.push({ proposal, kind: 'updated', at: proposal.updated_at })
    }
  }
  return notices.sort((a, b) => timeOf(b.at) - timeOf(a.at))
}

export const latestNoticeTime = (notices: ProposalNotice[]) =>
  notices.reduce<string | null>(
    (latest, notice) =>
      latest === null || timeOf(notice.at) > timeOf(latest)
        ? notice.at
        : latest,
    null
  )
