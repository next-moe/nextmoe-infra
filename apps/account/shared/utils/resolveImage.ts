
interface ImageURLOptions {
  /** image_service CDN base, e.g. https://image.kungal.iloveren.link */
  cdnBase: string
  /** ext (default 'webp', V1 always outputs webp) */
  ext?: string
  /** variant suffix (e.g. '256', '100', 'mini'); when omitted, returns main URL */
  variant?: string
}

/** Build a public image_service URL from a content hash. */
export const imageHashUrl = (
  hash: string,
  opts: ImageURLOptions
): string => {
  const ext = opts.ext ?? 'webp'
  const base = opts.cdnBase.replace(/\/+$/, '')
  if (!hash || hash.length < 4) return ''
  const prefix = `${base}/${hash.slice(0, 2)}/${hash.slice(2, 4)}/${hash}`
  return opts.variant ? `${prefix}_${opts.variant}.${ext}` : `${prefix}.${ext}`
}

/**
 * Resolve a user's avatar URL. Prefers `avatar_image_hash` (new), falls
 * back to legacy `avatar`, finally to the placeholder.
 *
 * @param user object with `avatar` and optional `avatar_image_hash`
 * @param opts.cdnBase  image_service CDN base
 * @param opts.variant  e.g. '256' (default avatar tile) or '100' (small)
 * @param placeholder   shown when neither hash nor legacy URL is set
 */
export const resolveAvatarUrl = (
  user: { avatar: string; avatar_image_hash?: string | null } | null | undefined,
  opts: ImageURLOptions,
  placeholder = ''
): string => {
  if (!user) return placeholder
  if (user.avatar_image_hash) {
    return imageHashUrl(user.avatar_image_hash, opts)
  }
  if (user.avatar) return legacyAvatarVariant(user.avatar, opts.variant)
  return placeholder
}

const legacyAvatarVariant = (url: string, variant?: string): string => {
  if (variant && /\/avatar\.webp$/.test(url)) {
    return url.replace(/\/avatar\.webp$/, '/avatar-100.webp')
  }
  return url
}

/**
 * Resolve a galgame's banner URL.
 *
 * Preference order (matches the galgame backend's effective-banner derivation):
 *   1. effective_banner_hash — image_hash of the pinned cover
 *      (galgame_cover.sort_order=0), computed server-side. PR5 retired
 *      the legacy banner_image_hash column; this is now the SOLE
 *      image_service banner reference.
 *   2. banner — original VNDB / user-provided URL (oldest fallback,
 *      used when the galgame has no covers yet).
 *
 * This apps/web copy is currently unused (admin web has no galgame UI) but
 * exported for any future cross-app consumer; the apps/wiki copy it once
 * mirrored retired at E3b.
 */
export const resolveBannerUrl = (
  galgame:
    | {
        banner: string
        effective_banner_hash?: string | null
      }
    | null
    | undefined,
  opts: ImageURLOptions,
  placeholder = ''
): string => {
  if (!galgame) return placeholder
  if (galgame.effective_banner_hash) {
    return imageHashUrl(galgame.effective_banner_hash, opts)
  }
  if (galgame.banner) return galgame.banner
  return placeholder
}
