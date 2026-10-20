import type { CatalogUserRef } from '#shared/types/catalog'

export const userChipOf = (user: CatalogUserRef | null | undefined) =>
  user
    ? { id: Number(user.id) || 0, name: user.name, avatar: user.avatar ?? '' }
    : null
