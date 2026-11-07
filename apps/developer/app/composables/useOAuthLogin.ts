const authUrl = (path: '/auth/login' | '/auth/register', redirect?: string) =>
  redirect ? `${path}?${new URLSearchParams({ redirect })}` : path

export const useOAuthLogin = () => {
  const startLogin = (redirect?: string) => {
    window.location.href = authUrl('/auth/login', redirect)
  }

  const startRegister = (redirect?: string) => {
    window.location.href = authUrl('/auth/register', redirect)
  }

  return { startLogin, startRegister }
}
