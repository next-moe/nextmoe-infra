import type { components } from './generated/catalog-v2-api'

export interface ActivityUser {
  object?: 'user'
  id: string
  name: string
  avatar?: string | null
}

type ProposalRecord = components['schemas']['ProposalRecord']
type RevisionRecord = components['schemas']['Revision']

export type ActivityProposal = ProposalRecord & {
  proposer?: ActivityUser | null
  decided_by?: ActivityUser | null
}

export type ActivityRevision = RevisionRecord & {
  actor?: ActivityUser | null
}

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
  proposal: ActivityProposal
  kind: ProposalNoticeKind
  at: string
}
