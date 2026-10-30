import type { components, operations } from './generated/ai-admin-api'

type Schemas = components['schemas']

export type LlmStatus = Schemas['LlmStatus']

export type LlmProvider = Schemas['ProviderView']
export type LlmProviderCreateRequest = Schemas['ProviderInput'] & {
  name: string
  base_url: string
}
export type LlmProviderPatchRequest = Schemas['ProviderInput']

export type LlmModel = Schemas['ModelView']
export type LlmModelKind = LlmModel['kind']
export type LlmModelCreateRequest = Required<
  Omit<Schemas['ModelInput'], '$schema'>
>
export type LlmModelPatchRequest = Schemas['ModelInput']

export type LlmPrice = Schemas['Price']
export type LlmRouteStats = Schemas['RouteStats']
export type LlmRoute = Schemas['RouteView']
export type LlmRouteStatus = LlmRoute['status']
export type LlmRouteCreateRequest = Schemas['RouteInput'] & {
  model_id: number
  provider_id: number
  upstream_id: string
}
export type LlmRoutePatchRequest = Schemas['RouteInput']
export type LlmRouteTestResult = Schemas['ProbeResult']

export type LlmScene = Schemas['SceneView']
export type LlmSceneClass = LlmScene['class']
export type LlmSceneStrategy = LlmScene['strategy']
export type LlmSceneReasoning = LlmScene['reasoning']
export type LlmThinkingControl = LlmRoute['thinking_control']
export type LlmScenePutRequest = Required<
  Omit<Schemas['SceneInput'], '$schema'>
> & {
  context_fallback_model_ids: number[]
  content_fallback_model_ids: number[]
}
export type LlmSceneChainKey =
  | 'fallback_model_ids'
  | 'context_fallback_model_ids'
  | 'content_fallback_model_ids'
export type LlmScenePlan = Schemas['ScenePlan']
export type LlmPlanStep = Schemas['PlanStep']
export type LlmPlanStepState = LlmPlanStep['state']

export type LlmRouteHealth = Schemas['RouteHealthView']
export type LlmRouteHealthState = LlmRouteHealth['state']

export type LlmAccessKey = Schemas['AccessKeyView']
export type LlmAccessKeyCreateRequest = Schemas['AccessKeyInput'] & {
  name: string
}
export type LlmAccessKeyCreated = Schemas['CreatedAccessKey']
export type LlmAccessKeyPatchRequest = Schemas['AccessKeyInput']

export type LlmEvent = Schemas['EventView']

export type LlmPriceKey = keyof LlmPrice
export type LlmPriceDraft = Record<LlmPriceKey, string>
export type LlmRouteProtocol = LlmRoute['protocol']

export type LlmCatalogEntry = Schemas['ProviderModelView']
export type LlmCatalogKind = LlmCatalogEntry['kind']
export type LlmCatalogKindSource = LlmCatalogEntry['kind_source']
export type LlmCatalogKindChoice =
  Schemas['UpdateLLMProviderModelRequest']['kind']
export type LlmCatalogSyncResult = Schemas['SyncResult']
export type LlmCatalogAdoptRequest = Omit<Schemas['AdoptInput'], '$schema'>
export type LlmCatalogAdoptResult = Schemas['AdoptResult']

export type LlmUsageQuery = NonNullable<
  operations['getLLMUsage']['parameters']['query']
>
export type LlmUsageDays = NonNullable<LlmUsageQuery['days']>
export type LlmUsageGroup = NonNullable<LlmUsageQuery['group']>
export type LlmUsageReport = Schemas['UsageReport']
export type LlmUsageRow = Schemas['UsageRow']
export type LlmUsageDay = Schemas['UsageDay']

export type LlmGatewayConfig = Omit<Schemas['GatewayConfig'], '$schema'>
export type LlmImportResult = Schemas['ImportResult']
export type LlmImportChange = Schemas['ImportChange']
export type LlmImportKind = LlmImportChange['kind']
export type LlmImportAction = LlmImportChange['action']
