export const isSafeInternalPath = (r?: string | null): r is string => {
  if (!r || !r.startsWith('/')) return false
  try {
    return new URL(r, 'https://guard.internal').origin === 'https://guard.internal'
  } catch {
    return false
  }
}
