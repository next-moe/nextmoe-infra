import type { components } from './generated/trust-admin-api'

type Schemas = components['schemas']

export type TrustReviewItem = Schemas['ReviewItemView']
export type TrustReviewItemPage = Schemas['PageReviewItemView']
export type TrustReviewItemDetail = Schemas['ReviewItemDetail']
export type TrustDecideData = Schemas['DecideData']

export type TrustSubjectKind = Schemas['SubjectKindView']
export type TrustReason = Schemas['ReasonView']
export type TrustCreateSubjectKindRequest = Schemas['CreateSubjectKindRequest']
export type TrustPatchSubjectKindRequest = Schemas['PatchSubjectKindRequest']
export type TrustEnsureSubjectKindItem = Schemas['EnsureSubjectKindItem']
export type TrustBatchSubjectKindsRequest = Schemas['BatchSubjectKindsRequest']
export type TrustEnsureSubjectKindsResponse =
  Schemas['EnsureSubjectKindsResponse']
export type TrustEnsureSubjectKindResult =
  Schemas['EnsureSubjectKindResultView']
export type TrustCreateReasonRequest = Schemas['CreateReasonRequest']
export type TrustPatchReasonRequest = Schemas['PatchReasonRequest']

export type TrustTerm = Schemas['TermView']
export type TrustTermsResponse = Schemas['TermsResponse']
export type TrustCreateTermRequest = Schemas['CreateTermRequest']

export type TrustDispositionPage = Schemas['PageDispositionView']

export type TrustSitePolicy = Schemas['SitePolicyView']
export type TrustSitePoliciesResponse = Schemas['SitePoliciesResponse']
export type TrustUpsertSitePolicyRequest = Schemas['UpsertSitePolicyRequest']
