<script setup lang="ts">
import { API_BASE_URL } from '~/constants/dev'
import { LANDING_SAMPLE_PATH } from '~/constants/landing'

const base = useRuntimeConfig().public.nextmoeApiBase
const curlSample = `curl "${API_BASE_URL}/${LANDING_SAMPLE_PATH}"`

interface RunResult {
  status: number
  body: string
  quota: string
}

const running = ref(false)
const result = ref<RunResult | null>(null)

const run = async () => {
  if (running.value) return
  running.value = true
  try {
    const resp = await $fetch.raw(`${base}/v2/${LANDING_SAMPLE_PATH}`, {
      ignoreResponseError: true
    })
    const remaining = resp.headers.get('x-ratelimit-remaining')
    const limit = resp.headers.get('x-ratelimit-limit')
    result.value = {
      status: resp.status,
      body: JSON.stringify(resp._data, null, 2),
      quota: remaining && limit ? `本分钟还剩 ${remaining}/${limit} 次` : ''
    }
  } catch {
    result.value = {
      status: 0,
      body: '请求没有到达 API，请检查网络。',
      quota: ''
    }
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div
    class="grid gap-8 lg:grid-cols-[minmax(0,19rem)_minmax(0,1fr)] lg:items-center lg:gap-14"
  >
    <div>
      <h2 class="text-foreground text-2xl font-bold tracking-tight">
        不注册，现在就能调
      </h2>
      <p class="text-default-500 mt-3 text-sm leading-relaxed">
        读目录不需要账号，也不需要密钥。复制到终端，或者直接点「运行」：请求从你的浏览器发出，额度按你自己的
        IP 计。
      </p>
      <p class="text-default-500 mt-3 text-sm leading-relaxed">
        这条请求拿 Steam、DLsite、Bangumi 三家各自的 id
        去查，换回的是同一部作品。
      </p>
      <NuxtLink
        to="/docs/quickstart"
        class="text-primary mt-4 inline-flex items-center gap-1 text-sm font-medium hover:underline"
      >
        快速上手
        <KunIcon name="lucide:arrow-right" class="size-4" />
      </NuxtLink>
    </div>

    <div
      class="border-default-200 bg-content1 overflow-hidden rounded-xl border"
    >
      <div
        class="border-default-200 flex items-center gap-2 border-b px-4 py-2.5"
      >
        <span class="bg-danger/40 size-2.5 rounded-full" />
        <span class="bg-warning/40 size-2.5 rounded-full" />
        <span class="bg-success/40 size-2.5 rounded-full" />
        <span class="text-default-400 ml-1.5 truncate font-mono text-xs">
          {{ API_BASE_URL }}
        </span>
        <div class="ml-auto flex shrink-0 items-center gap-1">
          <DocsCopyButton :text="curlSample" label="复制 curl 示例" />
          <KunButton
            size="sm"
            color="primary"
            variant="flat"
            :disabled="running"
            @click="run"
          >
            <KunIcon name="lucide:send" class="mr-1 size-3.5" />
            {{ running ? '请求中…' : '运行' }}
          </KunButton>
        </div>
      </div>
      <pre
        class="text-default-600 overflow-x-auto px-4 py-4 text-left font-mono text-xs leading-relaxed sm:text-sm"
      ><code>{{ curlSample }}</code></pre>
      <div v-if="result" class="border-default-200 border-t">
        <p
          class="text-default-400 flex flex-wrap gap-x-3 px-4 pt-3 font-mono text-xs"
        >
          <span :class="result.status === 200 ? 'text-success' : 'text-danger'">
            {{ result.status ? `HTTP ${result.status}` : '网络错误' }}
          </span>
          <span v-if="result.quota">{{ result.quota }}</span>
        </p>
        <pre
          class="text-default-600 max-h-80 overflow-auto px-4 py-3 text-left font-mono text-xs leading-relaxed"
        ><code>{{ result.body }}</code></pre>
      </div>
    </div>
  </div>
</template>
