import type { NsfwDisplay } from '../types/preferences'

// Mirrors EffectiveNSFWDisplay in the Go model, which the deployed downstream
// sites also ship. The age attestation was retired on 2026-09-23 and
// adult_confirmed_at is now set on every account, so the first branch only
// still fires for a user object restored from a session older than the field.
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
