export const APPLICATION_REASON_MIN = 10
export const APPLICATION_REASON_MAX = 1000

const MINUTE_MS = 60_000

export const reapplyWaitMs = (
  reapplyAfter: string | null | undefined,
  now = Date.now()
) => {
  if (!reapplyAfter) {
    return 0
  }
  const until = Date.parse(reapplyAfter)
  return Number.isNaN(until) ? 0 : Math.max(until - now, 0)
}

export const formatWait = (ms: number) => {
  const minutes = Math.ceil(ms / MINUTE_MS)
  if (minutes < 60) {
    return `${minutes} 分钟`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    const rest = minutes % 60
    return rest ? `${hours} 小时 ${rest} 分钟` : `${hours} 小时`
  }
  const days = Math.floor(hours / 24)
  const restHours = hours % 24
  return restHours ? `${days} 天 ${restHours} 小时` : `${days} 天`
}
