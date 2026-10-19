<script setup lang="ts">
import { AUTH_ERROR_MESSAGES } from '~/constants/auth'

useHead({ title: '登录未完成 · 未萌编辑台' })

const route = useRoute()
const config = useRuntimeConfig()

const reason = computed(() =>
  typeof route.query.reason === 'string' ? route.query.reason : ''
)
const message = computed(
  () =>
    AUTH_ERROR_MESSAGES[reason.value] ?? '登录过程中出现了问题，请稍后再试。'
)
const retryTo = computed(() => {
  const target =
    typeof route.query.return === 'string' ? route.query.return : '/'
  return target.startsWith('/') && !target.startsWith('//') ? target : '/'
})

const retry = () => {
  window.location.assign(retryTo.value)
}
</script>

<template>
  <KunCard bordered padding="lg" class-name="w-full max-w-md text-center">
    <div class="flex flex-col items-center gap-4">
      <img src="/favicon.webp" alt="" class="size-12 rounded-xl" />
      <div class="space-y-1">
        <h1 class="text-lg font-semibold">未萌编辑台暂时无法进入</h1>
        <p class="text-default-500 text-sm">{{ message }}</p>
      </div>
      <div class="flex flex-wrap justify-center gap-2">
        <KunButton color="primary" @click="retry">重试</KunButton>
        <KunButton
          variant="bordered"
          :href="String(config.public.oauthWebBase)"
        >
          前往账号中心
        </KunButton>
      </div>
      <p v-if="reason" class="text-default-300 font-mono text-xs">
        {{ reason }}
      </p>
    </div>
  </KunCard>
</template>
