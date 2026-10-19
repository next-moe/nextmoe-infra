export interface EditQuotaBucket {
  used: number
  limit: number
}

export interface EditQuota {
  proposals?: EditQuotaBucket
}
