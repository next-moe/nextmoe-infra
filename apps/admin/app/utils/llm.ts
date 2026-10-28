export const llmUsdPerM = (micro?: number | null) =>
  micro == null ? '—' : `$${(micro / 1e6).toFixed(4).replace(/\.?0+$/, '')}`

export const llmCostUsd = (nano?: number | null) =>
  `$${((nano ?? 0) / 1e9).toFixed(4)}`

export const llmTime = (iso?: string | null) =>
  iso ? new Date(iso).toLocaleString() : '—'

export const llmUsdToMicro = (raw: string): number | null => {
  const s = raw.trim()
  if (s === '') return null
  const n = Number(s)
  return Number.isFinite(n) && n >= 0 ? Math.round(n * 1e6) : NaN
}

export const llmMicroToUsd = (micro?: number | null) =>
  micro == null ? '' : String(micro / 1e6)

export const llmPpmToMultiplier = (ppm: number) =>
  `${(ppm / 1e6).toFixed(2).replace(/\.?0+$/, '')}×`
