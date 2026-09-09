export type JobStatus = 'running' | 'success' | 'failed' | 'skipped'
export type JobTrigger = 'schedule' | 'admin'

export interface JobRun {
  id: number
  job_name: string
  trigger: JobTrigger
  status: JobStatus
  summary?: Record<string, unknown> | null
  error?: string
  started_at: string
  finished_at?: string | null
  created_at: string
}

export interface JobInfo {
  name: string
  desc: string
  schedule: string
  enabled: boolean
  latest_run: JobRun | null
}
