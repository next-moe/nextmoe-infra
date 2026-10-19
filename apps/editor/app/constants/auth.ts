export const AUTH_ERROR_MESSAGES: Record<string, string> = {
  access_denied: '账号中心拒绝了这次登录，可能是账号被限制或授权被取消。',
  invalid_state: '登录请求已过期或在另一个标签页中完成了，重试即可。',
  exchange_failed: '账号中心暂时没有响应，请稍后重试。',
  server_error: '账号中心出现了错误，请稍后重试。',
  temporarily_unavailable: '账号中心正在维护，请稍后重试。'
}
