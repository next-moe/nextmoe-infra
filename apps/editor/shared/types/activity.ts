import type { CatalogProposal } from './catalog'

export interface ActivityList<T> {
  items?: T[]
  total?: number
}

export interface LocalDraft {
  key: string
  family: string
  entityId: string
  savedAt: string
  fieldKeys: string[]
}

export type ProposalNoticeKind = 'merged' | 'declined' | 'updated'

export interface ProposalNotice {
  proposal: CatalogProposal
  kind: ProposalNoticeKind
  at: string
}
