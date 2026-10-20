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

export interface CatalogEntitySummary {
  id: string
  display_name?: string
  title?: string
  latin?: string
  image?: CatalogEntityPicture | null
  photo?: CatalogEntityPicture | null
  logo?: CatalogEntityPicture | null
  [key: string]: unknown
}

export interface CatalogEntityList {
  items: CatalogEntitySummary[]
  next_cursor?: string | null
  total?: number | null
}
