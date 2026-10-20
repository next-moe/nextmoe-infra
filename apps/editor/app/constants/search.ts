import { ENTITY_META, type EditFamily } from './editEntities'

export interface SearchFamily {
  object: EditFamily
  label: string
  icon: string
  path: string
}

export const SEARCH_FAMILIES: SearchFamily[] = Object.values(ENTITY_META)
  .filter((meta) => meta.searchable)
  .map((meta) => ({
    object: meta.family,
    label: meta.label,
    icon: meta.icon,
    path: meta.path
  }))
