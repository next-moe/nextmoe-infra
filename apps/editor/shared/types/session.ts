export interface EditorSession {
  id: number
  uuid: string
  name: string
  avatar: string
  roles: string[]
  siteRoles: string[]
  canReview: boolean
}
