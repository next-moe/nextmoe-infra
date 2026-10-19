export interface EditProblemFieldError {
  pointer?: string
  parameter?: string
  header?: string
  reason: string
  detail: string
}

export interface EditProblem {
  status: number
  code: string
  title: string
  detail: string
  errors: EditProblemFieldError[]
}

const REASON_LABELS: Record<string, string> = {
  REQUIRED: '缺少必填内容',
  INVALID_FORMAT: '格式不正确',
  OUT_OF_RANGE: '超出允许范围',
  TOO_LONG: '内容过长',
  TOO_SHORT: '内容过短',
  TOO_MANY_ITEMS: '数量超出上限',
  TOO_FEW_ITEMS: '数量不足',
  DUPLICATE_ITEM: '有重复项',
  UNKNOWN_VALUE: '取值无效',
  NOT_ALLOWED_VALUE: '取值不被允许',
  UNKNOWN_REFERENCE: '引用的条目不存在',
  IMMUTABLE: '这一项已锁定，不接受修改',
  INCONSISTENT_WITH: '与其他内容冲突',
  NOT_PERMITTED: '你没有修改这一项的权限'
}

const CODE_MESSAGES: Record<string, string> = {
  QUOTA_EXCEEDED: '今天的提案额度已用完，或待审提案已达上限',
  RATE_LIMITED: '操作太频繁，请稍后再试',
  PRECONDITION_FAILED: '内容已被他人更新，请刷新后再试',
  PRECONDITION_REQUIRED: '缺少版本校验信息，请刷新后再试',
  NOT_FOUND: '条目不存在',
  ENTITY_MERGED: '这个条目已被合并到另一个条目',
  SITE_NOT_BOUND: '当前登录状态不能提交修改',
  SCOPE_REQUIRED: '当前登录状态不能提交修改，请重新登录',
  PERMISSION_REQUIRED: '你没有执行这项操作的权限',
  INVALID_STATE_TRANSITION: '这条提案已不在可操作的状态',
  IDEMPOTENCY_REQUEST_IN_PROGRESS: '上一次提交仍在处理中，请稍候',
  PAYLOAD_TOO_LARGE: '文件过大',
  UNSUPPORTED_MEDIA_TYPE: '不支持的文件格式'
}

export const parseEditProblem = (error: unknown): EditProblem => {
  const err = error as {
    statusCode?: number
    status?: number
    data?: Partial<EditProblem> | null
  }
  const data = err?.data ?? {}
  return {
    status: Number(data.status ?? err?.statusCode ?? err?.status ?? 0),
    code: String(data.code ?? ''),
    title: String(data.title ?? ''),
    detail: String(data.detail ?? ''),
    errors: Array.isArray(data.errors) ? data.errors : []
  }
}

export const editProblemMessage = (problem: EditProblem) => {
  if (problem.code && CODE_MESSAGES[problem.code]) {
    return CODE_MESSAGES[problem.code]!
  }
  if (problem.status === 0) {
    return '网络异常，请稍后再试'
  }
  if (problem.status >= 500) {
    return '资料库暂时不可用，请稍后再试'
  }
  return problem.detail || problem.title || '提交失败'
}

const POINTER_PATCH_KEY = /^\/patch\/([^/]+)/

export const fieldKeyOfPointer = (pointer: string | undefined) => {
  const match = (pointer ?? '').match(POINTER_PATCH_KEY)
  return match
    ? decodeURIComponent(match[1]!.replace(/~1/g, '/').replace(/~0/g, '~'))
    : null
}

export const editProblemFieldErrors = (problem: EditProblem) => {
  const out: Record<string, string[]> = {}
  for (const entry of problem.errors) {
    const key = fieldKeyOfPointer(entry.pointer)
    if (!key) {
      continue
    }
    const label = REASON_LABELS[entry.reason] ?? entry.reason
    const message = entry.detail ? `${label}：${entry.detail}` : label
    out[key] = [...(out[key] ?? []), message]
  }
  return out
}
