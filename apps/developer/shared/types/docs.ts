
export type DocsMethod = 'get' | 'post' | 'put' | 'patch' | 'delete'

export type DocsFaceKey = 'v2' | 'moyu' | 'sticker'

export interface DocsAuth {
  kind: 'api_key' | 'user_token' | 'none'
  curl: string
  display: string
  note: string
}

export interface DocsSchemaNode {
  name?: string
  type: string
  format?: string
  doc?: string
  required?: boolean
  nullable?: boolean
  enum?: string[]
  children?: DocsSchemaNode[]
  itemsOf?: DocsSchemaNode
}

export interface DocsParam {
  name: string
  in: 'query' | 'path' | 'header'
  required: boolean
  type: string
  format?: string
  doc?: string
  enum?: string[]
}

export interface DocsResponse {
  status: string
  description: string
  schema?: DocsSchemaNode
}

export interface DocsOperation {
  id: string
  method: DocsMethod
  path: string
  summary: string
  description?: string
  /** Empty when the operation needs no credential — see `auth`. */
  scope: string
  /** Present only where the operation's credential differs from its face's. */
  auth?: DocsAuth
  params: DocsParam[]
  requestBody?: DocsSchemaNode
  responses: DocsResponse[]
  curl: string
}

export interface DocsGroup {
  key: string
  label: string
  operations: DocsOperation[]
}

export interface DocsFace {
  key: DocsFaceKey
  /** Short name, for tabs and breadcrumbs. */
  label: string
  /** Full display name, for headings and cards. */
  name: string
  baseUrl: string
  prefix: string
  /** Live machine-readable OpenAPI URL; absent when the face serves none. */
  specUrl?: string
  auth: DocsAuth
  notes?: string[]
  groups: DocsGroup[]
}

export interface DocsModel {
  faces: DocsFace[]
}
