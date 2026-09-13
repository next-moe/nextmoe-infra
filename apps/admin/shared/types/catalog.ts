import type { components } from './generated/catalog-admin-api'

type Schemas = components['schemas']

export type CatalogEntityRef = Schemas['EntityRefSummary']
export type CatalogEntitySummary = Schemas['EntitySummary']
export type CatalogCandidateItem = Schemas['CandidateItem']
export type CatalogCandidatePage = Schemas['PageCandidateItem']
export type CatalogCandidateBucket = Schemas['CandidateBucketCount']
export type CatalogProbableRefBucket = Schemas['ProbableRefBucketCount']
export type CatalogQueueSummary = Schemas['QueueSummary']

export type CatalogMergeDirection = 'ab' | 'ba'

export type CatalogProposalItem = Schemas['ProposalItem']
export type CatalogProbableRefItem = Schemas['ProbableRefItem']
export type CatalogProposalPage = Schemas['PageProposalItem']
export type CatalogProbableRefPage = Schemas['PageProbableRefItem']
export type CatalogDecideCandidateData = Schemas['DecideCandidateData']
export type CatalogDetachNameData = Schemas['DetachNameData']
export type CatalogProposalActionData = Schemas['ProposalActionData']
export type CatalogRefActionData = Schemas['RefActionData']
export type CatalogImageReferenceItem = Schemas['ImageReferenceItem']
export type CatalogImageReferencesData = Schemas['ImageReferencesData']
export type CatalogDetachImageReferencesData =
  Schemas['DetachImageReferencesData']
