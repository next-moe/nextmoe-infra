import { API_CODE_VALIDATION_FAILED } from '~/constants/devapi'

// This console and the developer console delete the same oauth_clients row
// through the same guard, and a refusal can come from any of its conditions —
// but this list's DTO carries no owner_user_id, so the row cannot tell them
// apart. Name the possibilities rather than re-derive the rule here: a second
// copy of it would drift from the one that actually runs.
export const oauthClientDeleteMessage = (
  code: number,
  fallback: string
): string => {
  if (code !== API_CODE_VALIDATION_FAILED) return fallback || '删除失败'
  return '该客户端不可删除：它能用于用户登录、绑定了站点，或者名下还有开发者密钥、调用记录、商店短链或未过期的会话。若它是开发者应用，请先在开发者控制台归档。'
}
