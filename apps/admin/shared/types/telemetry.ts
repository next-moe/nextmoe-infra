import type { components } from './generated/telemetry-admin-api'

type Schemas = components['schemas']

export type TelemetryAlertChannelTestResult = Schemas['AlertChannelTestResult']
export type TelemetryAlertChannel = Schemas['AlertChannelView']
export type TelemetryAlertSettings = Schemas['AlertSettings']
export type TelemetryAlertSettingsInput = Schemas['AlertSettingsInput']
export type TelemetryAlert = Schemas['AlertView']
export type TelemetryApp = Schemas['AppView']
export type TelemetryCreateAlertChannelRequest =
  Schemas['CreateAlertChannelRequest']
export type TelemetryCreateAppRequest = Schemas['CreateAppRequest']
export type TelemetryDailyMetric = Schemas['DailyMetricView']
export type TelemetryEngineSymbol = Schemas['EngineSymbolView']
export type TelemetryIssueCrash = Schemas['IssueCrashView']
export type TelemetryIssueDaily = Schemas['IssueDailyView']
export type TelemetryIssueDayPoint = Schemas['IssueDayPoint']
export type TelemetryIssueDetail = Schemas['IssueDetail']
export type TelemetryIssue = Schemas['IssueListItem']
export type TelemetryIssueView = Schemas['IssueView']
export type TelemetrySymbolFile = Schemas['SymbolFileView']
export type TelemetrySymbolUpload = Schemas['SymbolUploadView']
export type TelemetrySymbolsToken = Schemas['SymbolsTokenView']
export type TelemetryUpdateAlertChannelRequest =
  Schemas['UpdateAlertChannelRequest']
export type TelemetryUpdateAppRequest = Schemas['UpdateAppRequest']
export type TelemetryUpdateIssueRequest = Schemas['UpdateIssueRequest']
