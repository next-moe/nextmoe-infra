import type { components } from './generated/agent-api'

type Schemas = components['schemas']

export type AgentSiteTool = Schemas['SiteToolView']
export type AgentSiteToolAuth = AgentSiteTool['auth']
export type AgentSiteToolInput = Omit<Schemas['SiteToolInput'], '$schema'>
export type AgentSiteToolForm = Required<AgentSiteToolInput>
export type AgentSiteToolProbe = Schemas['SiteToolProbe']
export type AgentSiteToolListing = Schemas['SiteToolListing']
