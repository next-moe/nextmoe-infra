import type { components } from './generated/agent-api'

type Schemas = components['schemas']

export type AgentSiteTool = Schemas['SiteToolView']
export type AgentSiteToolAuth = AgentSiteTool['auth']
export type AgentSiteToolInput = Omit<Schemas['SiteToolInput'], '$schema'>
export type AgentSiteToolForm = Required<AgentSiteToolInput>
export type AgentSiteToolProbe = Schemas['SiteToolProbe']
export type AgentSiteToolListing = Schemas['SiteToolListing']

export type AgentPersona = Schemas['PersonaView']
export type AgentPersonaList = Schemas['PersonaList']
export type AgentPersonaInput = Omit<Schemas['PersonaInput'], '$schema'>
export type AgentSuggestionGroup = Schemas['SuggestionGroup']
export type AgentSuggestionDraft = Omit<AgentSuggestionGroup, 'prompts'> & {
  promptsText: string
}
