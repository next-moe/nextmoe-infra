import type { JobStatus, JobTrigger } from '~~/shared/types/jobs'

export const JOB_STATUS_MAP: Record<
  JobStatus,
  { label: string; color: 'info' | 'success' | 'danger' | 'default' }
> = {
  running: { label: '运行中', color: 'info' },
  success: { label: '成功', color: 'success' },
  failed: { label: '失败', color: 'danger' },
  skipped: { label: '跳过', color: 'default' }
}

export const jobStatusMeta = (s: string) =>
  JOB_STATUS_MAP[s as JobStatus] ?? { label: s, color: 'default' as const }

export const JOB_TRIGGER_LABEL: Record<JobTrigger, string> = {
  schedule: '定时',
  admin: '手动'
}

export const jobTriggerLabel = (t: string) =>
  JOB_TRIGGER_LABEL[t as JobTrigger] ?? t

export const JOB_DISABLED_LABEL = '已停用'

export const formatJobSchedule = (s: string): string => {
  const daily = /^daily@(\d{2}:\d{2})$/.exec(s)
  if (daily?.[1]) return `每日 ${daily[1]} (UTC)`

  const minutes = /^every:(\d+)m$/.exec(s)
  if (minutes?.[1]) return `每 ${minutes[1]} 分钟`

  const hours = /^every:(\d+)h$/.exec(s)
  if (hours?.[1]) return `每 ${hours[1]} 小时`

  return s
}
