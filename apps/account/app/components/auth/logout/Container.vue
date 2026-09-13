<script setup lang="ts">
import { AUTH_ART } from '~/constants/auth-art'

const route = useRoute()
const auth = useAuth()
const api = useApi()

onMounted(async () => {
  //    /oauth/authorize no longer silently auto-consents).
  await auth.logoutSilent()

  const clientId = route.query.client_id as string | undefined
  const redirect = route.query.redirect as string | undefined
  const state = route.query.state as string | undefined
  let dest = '/'
  if (clientId && redirect) {
    try {
      const res = await api.get<{ url: string }>('/oauth/post-logout-redirect', {
        client_id: clientId,
        redirect,
      })
      if (res.code === 0 && res.data?.url) {
        dest = res.data.url
        if (state) {
          const u = new URL(dest)
          u.searchParams.set('state', state)
          dest = u.toString()
        }
      }
    } catch {
      // fall back to OP home
    }
  }
  window.location.href = dest
})
</script>

<template>
  <AuthShell :art="AUTH_ART.farewell">
    <div class="flex flex-col items-center py-10 text-center">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary mb-6 size-8 animate-spin"
      />
      <h1 class="text-foreground text-xl font-semibold tracking-tight">
        正在登出
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        正在清除登录状态并返回来源站点
      </p>
    </div>
  </AuthShell>
</template>
