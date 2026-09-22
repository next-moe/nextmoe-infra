import type { NsfwDisplay } from '../types/preferences'

// The server stores the choice and the attestation separately, and a row that
// predates the attestation carries the 'blur' backfill with no confirmation —
// so reading nsfw_display on its own would un-blur every account that never
// attested. Mirrors EffectiveNSFWDisplay in the Go model.
export const effectiveNsfwDisplay = (
  adultConfirmedAt: string | null | undefined,
  nsfwDisplay: NsfwDisplay | undefined
): NsfwDisplay => {
  if (!adultConfirmedAt || !nsfwDisplay) return 'hide'
  return nsfwDisplay
}

export const formatPreferenceSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export const formatPreferenceTime = (iso: string): string => {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}
