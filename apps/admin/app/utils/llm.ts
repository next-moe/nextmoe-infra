import { LLM_PRICE_FIELDS } from '~/constants/llm'
import type { LlmPrice, LlmPriceDraft } from '~~/shared/types/llm'

export const llmUsdPerM = (micro?: number | null) =>
  micro == null ? '—' : `$${(micro / 1e6).toFixed(4).replace(/\.?0+$/, '')}`

export const llmCostUsd = (nano?: number | null) =>
  `$${((nano ?? 0) / 1e9).toFixed(4)}`

export const llmTime = (iso?: string | null) =>
  iso ? new Date(iso).toLocaleString() : '—'

export const llmDate = (iso?: string | null) =>
  iso ? new Date(iso).toLocaleDateString() : '—'

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

const PRICE_KEYS = LLM_PRICE_FIELDS.map((f) => f.key)

export const llmPriceDraft = (p?: LlmPrice | null): LlmPriceDraft => ({
  input_micro: llmMicroToUsd(p?.input_micro),
  cached_micro: llmMicroToUsd(p?.cached_micro),
  output_micro: llmMicroToUsd(p?.output_micro),
  per_call_micro: llmMicroToUsd(p?.per_call_micro)
})

export const llmParsePrice = (
  draft: LlmPriceDraft
): Required<LlmPrice> | null | 'invalid' => {
  if (PRICE_KEYS.every((k) => draft[k].trim() === '')) return null
  const out: Required<LlmPrice> = {
    input_micro: 0,
    cached_micro: 0,
    output_micro: 0,
    per_call_micro: 0
  }
  for (const k of PRICE_KEYS) {
    const n = llmUsdToMicro(draft[k])
    if (Number.isNaN(n)) return 'invalid'
    out[k] = n ?? 0
  }
  return out
}

export const llmNanoUsd = (nano: number) => {
  const usd = nano / 1e9
  return `$${usd >= 1 ? usd.toFixed(2) : usd.toFixed(4)}`
}

export const llmBudgetDraft = (micro: number) =>
  micro > 0 ? String(micro / 1e6) : ''

export const llmParseBudget = (draft: string): number | 'invalid' => {
  const micro = llmUsdToMicro(draft)
  if (Number.isNaN(micro)) return 'invalid'
  return micro ?? 0
}

export const llmBudgetRatio = (spentNano: number, budgetMicro: number) =>
  budgetMicro > 0 ? spentNano / 1e3 / budgetMicro : 0

export const llmCstDays = (n: number) => {
  const today = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai'
  }).format(new Date())
  const base = Date.parse(`${today}T00:00:00Z`)
  return Array.from({ length: n }, (_, i) =>
    new Date(base - (n - 1 - i) * 86_400_000).toISOString().slice(0, 10)
  )
}

export const llmAdoptModelKey = (upstreamId: string) =>
  upstreamId
    .toLowerCase()
    .replaceAll('/', '-')
    .replace(/[^a-z0-9_.:-]+/g, '-')
    .replace(/^-+|-+$/g, '')
