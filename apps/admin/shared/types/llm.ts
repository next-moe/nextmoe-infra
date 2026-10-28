import type { components } from './generated/ai-admin-api'

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
export type LlmScenePutRequest = Schemas['SceneInput']

export type LlmAccessKey = Schemas['AccessKeyView']
export type LlmAccessKeyCreateRequest = Schemas['AccessKeyInput'] & {
  name: string
}
export type LlmAccessKeyCreated = Schemas['CreatedAccessKey']
export type LlmAccessKeyPatchRequest = Schemas['AccessKeyInput']

export type LlmEvent = Schemas['EventView']
