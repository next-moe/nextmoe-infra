import type { NameSource } from './preferences'
import type { components } from './generated/catalog-v2-api'

type Schemas = components['schemas']

export type CatalogWork = Schemas['Work']
export type CatalogWorkList = Schemas['ListWork']
export type CatalogWorkTitle = Schemas['WorkTitle']
export type CatalogIntro = Schemas['Intro']
export type CatalogWorkTag = Schemas['WorkTag']
export type CatalogWorkCompany = Schemas['WorkCompany']
export type CatalogWorkLink = Schemas['WorkLink']
export type CatalogRelation = Schemas['Relation']
export type CatalogCover = Schemas['Cover']
export type CatalogScreenshot = Schemas['Screenshot']
export type CatalogCreditGroup = Schemas['CreditGroup']
export type CatalogWorkCharacter = Schemas['WorkCharacter']
export type CatalogSearchHit = Schemas['SearchHit']
export type CatalogSearchList = Schemas['ListSearchHit']
export type CatalogRevision = Schemas['Revision']
export type CatalogRevisionList = Schemas['ListRevision']
export type CatalogProposal = Schemas['ProposalRecord']
export type CatalogMergeRequest = Schemas['MergeRequestRecord']
export type CatalogMergeRequestList = Schemas['ListMergeRequestRecord']
export type CatalogDeletionRequest = Schemas['DeletionRequestRecord']
export type CatalogDeletionRequestList = Schemas['ListDeletionRequestRecord']
export type CatalogDeletionPreview = Schemas['DeletionPreview']
export type CatalogMergeUndoReport = Schemas['MergeUndoReport']
export type CatalogSplitRequest = Schemas['SplitRequestRecord']
export type CatalogSplitRequestList = Schemas['ListSplitRequestRecord']
export type CatalogSplitPreview = Schemas['SplitPreview']
export type CatalogSplitPreviewRelease = Schemas['SplitPreviewRelease']
export type CatalogSplitPreviewRef = Schemas['SplitPreviewRef']
export type CatalogSplitTitle = Schemas['SplitTitle']
export type CatalogSplitRef = Schemas['SplitRef']
export type CatalogMergePreview = Schemas['MergePreview']
export type CatalogMergePreviewField = Schemas['MergePreviewField']
export type CatalogMergePreviewAnchor = Schemas['MergePreviewAnchor']
export type CatalogMergePreviewRelease = Schemas['MergePreviewRelease']
export type CatalogMergePreviewTitle = Schemas['MergePreviewTitle']
export type CatalogMergePreviewIntro = Schemas['MergePreviewIntro']
export type CatalogMergePreviewCount = Schemas['MergePreviewCount']
export type CatalogMergePreviewIssue = Schemas['MergePreviewIssue']
export type CatalogMergeCandidate = Schemas['MergeCandidate']
export type CatalogMergeCandidateList = Schemas['ListMergeCandidate']
export type CatalogProposalList = Schemas['ListProposalRecord']
export type CatalogObjectSchema = Schemas['ObjectSchema']
export type CatalogSchemaField = Schemas['SchemaField']
export type CatalogEditCapabilities = Schemas['EditCapabilities']
export type CatalogFieldCapability = Schemas['FieldCapability']
export type CatalogEditContext = Schemas['EditContext']
export type CatalogEditBundle = Schemas['EditBundle']
export type CatalogEditContextField = Schemas['EditContextField']
export type CatalogEditContextRow = Schemas['EditContextRow']
export type CatalogColumnProvenance = Schemas['ColumnProvenance']
export type CatalogUserRef = Schemas['UserRef']

export interface CatalogEntityPicture {
  url: string
  thumbhash?: string | null
}

export interface CatalogEntitySummary extends NameSource {
  id: string
  display_name?: string
  title?: string
  latin?: string
  image?: CatalogEntityPicture | null
  photo?: CatalogEntityPicture | null
  logo?: CatalogEntityPicture | null
  cover?: CatalogEntityPicture | null
  [key: string]: unknown
}

export interface CatalogEntityList {
  items: CatalogEntitySummary[]
  next_cursor?: string | null
  total?: number | null
}
