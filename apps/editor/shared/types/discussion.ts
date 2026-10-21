import type { components } from './generated/catalog-v2-api'

type Schemas = components['schemas']

export type DiscussionPost = Schemas['DiscussionPost']
export type DiscussionPage = Schemas['DiscussionPage']
export type DiscussionViewer = Schemas['DiscussionViewer']
export type DiscussionNotice = Schemas['DiscussionNotice']

export interface DiscussionParticipant {
  uid: string
  user?: Schemas['UserRef'] | null
  role?: string
}
