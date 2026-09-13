import type { components } from './generated/ai-admin-api'

type Schemas = components['schemas']

export type AiUsageSummary = Schemas['UsageSummary']
export type AiUsageOverview = Schemas['UsageOverview']
export type AiSummaryRow = Schemas['SummaryRow']

export type AiDailySeries = Schemas['DailySeries']
export type AiDailyPoint = Schemas['DailyPoint']

export type AiBudget = Schemas['BudgetView']
export type AiUpsertBudgetRequest = Schemas['UpsertBudgetRequest']
