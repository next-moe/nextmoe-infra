<script setup lang="ts">
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()

useKunDisableSeo(props.error.statusCode === 404 ? '页面不存在' : '出错了')

const handleClear = () => {
  clearError({ redirect: '/' })
}
</script>

<template>
  <div
    class="flex min-h-screen flex-col items-center justify-center bg-default-50 p-4"
  >
    <KunCard class="w-full max-w-md p-8 text-center">
      <h1 class="text-foreground text-2xl font-bold">
        {{ error.statusCode === 404 ? '页面不存在' : '出错了' }}
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        {{ error.statusMessage || error.message }}
      </p>
      <KunButton color="primary" class="mt-6" @click="handleClear">
        返回首页
      </KunButton>
    </KunCard>
  </div>
</template>
