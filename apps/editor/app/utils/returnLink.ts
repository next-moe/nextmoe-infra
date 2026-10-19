import type { ReturnSite } from '../constants/returnSites'

export interface ReturnLink {
  label: string
  url: string
}

export const resolveReturnLink = (
  from: unknown,
  target: unknown,
  sites: ReturnSite[]
): ReturnLink | null => {
  if (typeof from !== 'string' || typeof target !== 'string') {
    return null
  }
  const site = sites.find((s) => s.key === from)
  if (!site) {
    return null
  }
  let url: URL
  try {
    url = new URL(target)
  } catch {
    return null
  }
  if (url.protocol !== 'https:' || !site.origins.includes(url.origin)) {
    return null
  }
  return { label: site.label, url: url.toString() }
}
