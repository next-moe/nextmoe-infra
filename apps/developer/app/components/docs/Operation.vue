<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const route = useRoute()
const { findOperation } = useDocs()
const { t } = useDocsI18n()

const resolved = findOperation(
  route.params.face as string,
  route.params.operationId as string
)
if (!resolved) {
  throw createError({
    statusCode: 404,
    statusMessage: '未找到该端点',
    fatal: true
  })
}
const { face, operation } = resolved

const fullUrl = `${face.baseUrl}${operation.path}`
const auth = operation.auth ?? face.auth

useSeoMeta({
  title: `${operation.id} · ${face.name}`,
  description: t(operation.summary)
})

const openResp = reactive<Record<string, boolean>>({})
operation.responses.forEach((r) => {
  openResp[r.status] = r.status === '200'
})
const toggleResp = (status: string) => {
  openResp[status] = !openResp[status]
}

const statusMeta = (status: string): { label: string; class: string } => {
  if (status === 'default') {
    return { label: '默认（错误）', class: 'bg-danger-50 text-danger-600' }
  }
  const code = Number(status)
  if (code >= 200 && code < 300) {
    return { label: `${status} OK`, class: 'bg-success-50 text-success-600' }
  }
  if (code >= 400 && code < 500) {
    return { label: status, class: 'bg-warning-50 text-warning-600' }
  }
  return { label: status, class: 'bg-danger-50 text-danger-600' }
}
</script>

<template>
  <article class="space-y-8">
    <nav class="text-default-400 flex flex-wrap items-center gap-1.5 text-sm">
      <NuxtLink to="/docs" class="hover:text-foreground transition-colors">
        文档
      </NuxtLink>
      <KunIcon name="lucide:chevron-right" class="size-3.5" />
      <NuxtLink
        :to="`/docs/${face.key}`"
        class="hover:text-foreground transition-colors"
      >
        {{ face.label }}
      </NuxtLink>
      <KunIcon name="lucide:chevron-right" class="size-3.5" />
      <code class="text-default-500 font-mono">{{ operation.id }}</code>
    </nav>

    <header class="space-y-3">
      <h1 class="text-foreground text-2xl font-bold tracking-tight">
        {{ operation.id }}
      </h1>
      <p class="text-default-500">{{ t(operation.summary) }}</p>
      <p
        v-if="
          operation.description && operation.description !== operation.summary
        "
        class="text-default-400 text-sm leading-relaxed"
      >
        {{ t(operation.description) }}
      </p>

      <div
        class="border-default-200 bg-content1 flex items-center gap-3 overflow-x-auto rounded-xl border px-4 py-3"
      >
        <DocsMethodBadge :method="operation.method" size="md" />
        <code
          class="text-foreground flex-1 font-mono text-sm whitespace-nowrap"
        >
          {{ operation.path }}
        </code>
        <DocsCopyButton :text="fullUrl" label="复制请求地址" />
      </div>

      <div
        class="text-default-400 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs"
      >
        <span v-if="operation.scope" class="flex items-center gap-1.5">
          <KunIcon name="lucide:shield-check" class="size-3.5" />
          scope
          <code
            class="bg-default-100 text-default-600 rounded px-1 py-0.5 font-mono"
          >
            {{ operation.scope }}
          </code>
        </span>
        <span class="flex items-center gap-1.5">
          <KunIcon
            :name="
              auth.kind === 'none' || auth.kind === 'optional'
                ? 'lucide:lock-open'
                : 'lucide:key-round'
            "
            class="size-3.5"
          />
          <code v-if="auth.curl" class="font-mono">{{ auth.display }}</code>
          <span v-else class="text-success-600 font-medium">
            {{ auth.display }}
          </span>
          <span>（{{ auth.note }}）</span>
        </span>
      </div>
    </header>

    <section v-if="operation.params.length" class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">参数</h2>
      <DocsParamTable :params="operation.params" />
    </section>

    <section v-if="operation.requestBody" class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">请求体</h2>
      <p class="text-default-400 text-xs">
        <code class="font-mono">application/json</code>
      </p>
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <DocsSchemaTree :node="operation.requestBody" />
      </div>
    </section>

    <section class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">响应</h2>
      <div
        v-for="res in operation.responses"
        :key="res.status"
        class="border-default-200 overflow-hidden rounded-xl border"
      >
        <button
          type="button"
          class="bg-content1 hover:bg-default-50 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
          :aria-expanded="openResp[res.status]"
          @click="toggleResp(res.status)"
        >
          <span
            :class="
              cn(
                'rounded px-2 py-0.5 font-mono text-xs font-bold',
                statusMeta(res.status).class
              )
            "
          >
            {{ statusMeta(res.status).label }}
          </span>
          <span class="text-default-500 min-w-0 flex-1 truncate text-sm">
            {{ t(res.description) }}
          </span>
          <KunIcon
            v-if="res.schema"
            :name="
              openResp[res.status] ? 'lucide:chevron-up' : 'lucide:chevron-down'
            "
            class="text-default-400 size-4 shrink-0"
          />
        </button>
        <div
          v-if="res.schema && openResp[res.status]"
          class="border-default-200 border-t p-4"
        >
          <DocsSchemaTree :node="res.schema" />
        </div>
      </div>
    </section>

    <section class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">请求示例</h2>
      <DocsCurlBlock :code="operation.curl" />
    </section>
  </article>
</template>
