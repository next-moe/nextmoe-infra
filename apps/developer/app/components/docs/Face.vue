<script setup lang="ts">
import { DOCS_FACE_META } from '~/constants/docs'

const route = useRoute()
const { findFace, faceOperationCount } = useDocs()
const { t } = useDocsI18n()

const face = computed(() => findFace(route.params.face as string))

if (!face.value) {
  throw createError({
    statusCode: 404,
    statusMessage: '未找到这个 API',
    fatal: true
  })
}

const current = computed(() => face.value!)
const meta = computed(() => DOCS_FACE_META[current.value.key])

useSeoMeta({
  title: () => `${current.value.name} · API 文档`,
  description: () =>
    `${current.value.name} 的公开端点目录，共 ${faceOperationCount(current.value)} 个。`
})
</script>

<template>
  <div class="space-y-8">
    <nav class="text-default-400 flex items-center gap-1.5 text-sm">
      <NuxtLink to="/docs" class="hover:text-foreground transition-colors">
        文档
      </NuxtLink>
      <KunIcon name="lucide:chevron-right" class="size-3.5" />
      <span class="text-default-500">{{ current.label }}</span>
    </nav>

    <header>
      <div class="flex flex-wrap items-center gap-2">
        <h1 class="text-foreground text-2xl font-bold tracking-tight">
          {{ current.name }}
        </h1>
        <span
          v-if="meta.badge"
          class="bg-warning-50 text-warning-600 rounded-full px-2 py-0.5 text-xs font-medium"
        >
          {{ meta.badge }}
        </span>
      </div>
      <p class="text-default-500 mt-2 max-w-2xl text-sm leading-relaxed">
        {{ meta.tagline }}
      </p>
      <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        <span class="text-default-500">
          {{ faceOperationCount(current) }} 个公开端点
        </span>
        <code class="text-default-400 font-mono text-xs">
          {{ current.baseUrl }}{{ current.prefix }}
        </code>
        <code class="text-default-400 font-mono text-xs">
          {{ current.auth.display }}
        </code>
      </div>
      <ul
        v-if="current.notes?.length"
        class="border-default-200 bg-content1 mt-4 space-y-2 rounded-xl border p-4"
      >
        <li
          v-for="(note, i) in current.notes"
          :key="i"
          class="text-default-500 text-sm leading-relaxed"
        >
          {{ note }}
        </li>
      </ul>
    </header>

    <section v-for="group in current.groups" :key="group.key" class="space-y-3">
      <h2
        v-if="current.groups.length > 1"
        class="text-foreground text-sm font-semibold"
      >
        {{ group.label }}
      </h2>
      <ul
        class="divide-default-100 border-default-200 divide-y overflow-hidden rounded-xl border"
      >
        <li v-for="op in group.operations" :key="op.id">
          <NuxtLink
            :to="`/docs/${current.key}/${op.id}`"
            class="hover:bg-default-50 flex items-start gap-3 px-4 py-3 transition-colors"
          >
            <DocsMethodBadge :method="op.method" size="md" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <code class="text-foreground font-mono text-sm">{{
                  op.path
                }}</code>
                <span
                  v-if="op.auth?.kind === 'none'"
                  class="bg-success-50 text-success-600 rounded px-1.5 py-0.5 text-xs font-medium"
                >
                  无需凭据
                </span>
                <span
                  v-else-if="op.auth?.kind === 'optional'"
                  class="bg-success-50 text-success-600 rounded px-1.5 py-0.5 text-xs font-medium"
                >
                  可匿名
                </span>
              </div>
              <p class="text-default-500 mt-0.5 text-sm leading-relaxed">
                {{ t(op.summary) }}
              </p>
            </div>
            <KunIcon
              name="lucide:chevron-right"
              class="text-default-300 mt-1 size-4 shrink-0"
            />
          </NuxtLink>
        </li>
      </ul>
    </section>
  </div>
</template>
