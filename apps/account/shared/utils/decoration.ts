import type { KunAvatarDecoration } from '@kungal/ui-vue'
import type { ShopDecoration } from '../types/shop'

export const toAvatarDecoration = (
  d: ShopDecoration | null | undefined
): KunAvatarDecoration | null =>
  d?.static_url ? { src: d.static_url, animatedSrc: d.animated_url } : null
