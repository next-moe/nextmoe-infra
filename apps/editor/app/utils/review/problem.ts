import {
  editProblemMessage,
  parseEditProblem,
  type ProblemLabels
} from '#shared/utils/editProblem'
import { fieldLabel, memberLabel } from '~/utils/format'

interface ProblemBody {
  title?: string
  detail?: string
  code?: string
  deleted_reason?: string
  deleted_at?: string
  errors?: { detail?: string; pointer?: string; reason?: string }[]
}

interface FetchFailure {
  status?: number
  statusCode?: number
  data?: ProblemBody
  response?: { status?: number }
}

export const problemStatus = (error: unknown) => {
  const failure = error as FetchFailure | null
  return (
    failure?.statusCode ?? failure?.status ?? failure?.response?.status ?? 0
  )
}

export const PROBLEM_LABELS: ProblemLabels = {
  field: fieldLabel,
  member: memberLabel
}

export const problemMessage = (
  error: unknown,
  fallback = '操作失败，请稍后再试'
) =>
  (error as FetchFailure | null)?.data
    ? editProblemMessage(parseEditProblem(error), PROBLEM_LABELS, fallback)
    : fallback

export const problemCode = (error: unknown) =>
  (error as FetchFailure | null)?.data?.code ?? ''

export const deletedProblem = (error: unknown) => {
  const body = (error as FetchFailure | null)?.data
  return body?.code === 'ENTITY_DELETED'
    ? { reason: body.deleted_reason ?? '', deletedAt: body.deleted_at ?? '' }
    : null
}

export const conflictFieldKeys = (error: unknown) =>
  ((error as FetchFailure | null)?.data?.errors ?? [])
    .map((entry) => entry.pointer ?? '')
    .filter((pointer) => pointer.startsWith('/fields/'))
    .map((pointer) => pointer.slice('/fields/'.length))

export const mergeConflictKeys = (error: unknown) =>
  ((error as FetchFailure | null)?.data?.errors ?? [])
    .filter(
      (entry) =>
        entry.reason === 'inconsistent_with' &&
        (entry.pointer ?? '').startsWith('/patch/')
    )
    .map((entry) => (entry.pointer ?? '').slice('/patch/'.length))

export const isStale = (error: unknown) => {
  const status = problemStatus(error)
  return status === 412 || status === 409
}

export const newIdempotencyKey = () =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`
