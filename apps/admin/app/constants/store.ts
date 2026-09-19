import type { CouponBatchStatus } from '~~/shared/types/store'

export const STORE_COUPON_FACE_PRESETS = [5000, 1000] as const

export const COUPON_BATCH_STATUS: Record<
  CouponBatchStatus,
  { label: string; color: 'warning' | 'success' }
> = {
  draft: { label: '草稿', color: 'warning' },
  published: { label: '已发布', color: 'success' },
}

export const formatCount = (n: number) => n.toLocaleString('zh-CN')

export const formatShare = (ppm: number) => `${(ppm / 10_000).toFixed(2)}%`

export const dlsiteProductUrl = (productId: string) =>
  `https://www.dlsite.com/${productId.startsWith('VJ') ? 'pro' : 'maniax'}/work/=/product_id/${productId}.html`

export const parseCouponCodes = (raw: string) =>
  raw
    .split(/[\s,，;；]+/)
    .map((c) => c.trim())
    .filter(Boolean)

const pad = (n: number) => String(n).padStart(2, '0')

// Settlement days are JST calendar days whatever the operator's own zone.
export const jstDay = (d: Date) => {
  const jst = new Date(d.getTime() + 9 * 3600_000)
  return `${jst.getUTCFullYear()}-${pad(jst.getUTCMonth() + 1)}-${pad(jst.getUTCDate())}`
}

export const jstMonthRange = (offset: number): [string, string] => {
  const jst = new Date(Date.now() + 9 * 3600_000)
  const y = jst.getUTCFullYear()
  const m = jst.getUTCMonth() + offset
  const first = new Date(Date.UTC(y, m, 1))
  const last = offset === 0 ? jst : new Date(Date.UTC(y, m + 1, 0))
  const fmt = (d: Date) =>
    `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
  return [fmt(first), fmt(last)]
}

export const STORE_RANGE_PRESETS = [
  { id: 'this-month', label: '本月', range: () => jstMonthRange(0) },
  { id: 'last-month', label: '上月', range: () => jstMonthRange(-1) },
  {
    id: 'last-30',
    label: '近 30 天',
    range: (): [string, string] => [
      jstDay(new Date(Date.now() - 29 * 86400_000)),
      jstDay(new Date()),
    ],
  },
] as const
