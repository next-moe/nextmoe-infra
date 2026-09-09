import type { components } from './generated/artifact-admin-api'

type Schemas = components['schemas']

export type ArtifactAdminRow = Schemas['AdminArtifactRow']
export type ArtifactAdminListResponse = Schemas['AdminArtifactList']
export type ArtifactSiteStats = Schemas['AdminArtifactSiteStats']
export type ArtifactAdminStats = Schemas['AdminArtifactStats']
export type ArtifactStatus = ArtifactAdminRow['status']

export const ARTIFACT_STATUS_MAP: Record<
  ArtifactStatus,
  { label: string; color: 'success' | 'warning' | 'danger' }
> = {
  ready: { label: '就绪', color: 'success' },
  uploading: { label: '上传中', color: 'warning' },
  failed: { label: '失败', color: 'danger' }
}

export const ARTIFACT_STATUS_TABS = [
  { id: '', label: '全部', icon: 'lucide:list' },
  { id: 'ready', label: '就绪', icon: 'lucide:check' },
  { id: 'uploading', label: '上传中', icon: 'lucide:loader' },
  { id: 'failed', label: '失败', icon: 'lucide:x' }
] as const
