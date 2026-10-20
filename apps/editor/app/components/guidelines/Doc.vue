<script setup lang="ts">
import { GUIDELINE_CATEGORIES } from '~/constants/guidelines'
import { GUIDELINE_DOCS } from '~/generated/guidelines'

const QUOTA_DOC = 'roles-and-quotas'

const route = useRoute()

const index = computed(() =>
  GUIDELINE_DOCS.findIndex((doc) => doc.slug === String(route.params.slug))
)
const doc = computed(() => GUIDELINE_DOCS[index.value]!)
const category = computed(() =>
  GUIDELINE_CATEGORIES.find((entry) => entry.key === doc.value.category)
)
const previous = computed(() => GUIDELINE_DOCS[index.value - 1] ?? null)
const next = computed(() => GUIDELINE_DOCS[index.value + 1] ?? null)
const related = computed(() =>
  doc.value.related
    .map((slug) => GUIDELINE_DOCS.find((entry) => entry.slug === slug))
    .filter((entry) => entry !== undefined)
)

const showQuota = computed(() => doc.value.slug === QUOTA_DOC)
const { data: quota, pending: quotaPending } = useEditQuota()

useHead(() => ({ title: `${doc.value.title} · 编辑规范 · 未萌编辑台` }))
</script>

<template>
  <div class="mx-auto flex max-w-6xl gap-10 px-4 py-6 md:px-8 md:py-10">
    <article class="min-w-0 flex-1">
      <nav
        class="text-default-500 mb-3 flex items-center gap-1 text-sm"
        aria-label="面包屑"
      >
        <NuxtLink to="/guidelines" class="hover:text-foreground">
          编辑规范
        </NuxtLink>
        <KunIcon name="lucide:chevron-right" class="size-4" />
        <span>{{ category?.label }}</span>
      </nav>
      <header class="border-default-200 mb-6 border-b pb-6">
        <h1 class="flex items-center gap-3 text-2xl font-bold">
          <KunIcon :name="doc.icon" class="text-primary-500 size-7" />
          {{ doc.title }}
        </h1>
        <p class="text-default-600 mt-2 leading-7">{{ doc.description }}</p>
      </header>

      <section v-if="showQuota" class="mb-8" aria-label="你的当前额度">
        <DashboardQuotaCard :quota="quota ?? null" :loading="quotaPending" />
        <p class="text-default-500 mt-2 text-xs">
          上面是你账号现在的实际额度；下文的数字是默认值，管理员可以在配置中心调整。
        </p>
      </section>

      <KunContent :content="doc.html" class-name="guide-prose" />

      <section
        v-if="related.length"
        class="border-default-200 mt-12 border-t pt-6"
        aria-labelledby="guide-related"
      >
        <h2 id="guide-related" class="mb-3 font-semibold">相关规范</h2>
        <ul class="grid gap-3 sm:grid-cols-2">
          <li v-for="entry in related" :key="entry.slug">
            <NuxtLink
              :to="`/guidelines/${entry.slug}`"
              class="border-default-200 bg-content1 hover:border-primary-300 flex items-start gap-3 rounded-xl border p-4 transition-colors"
            >
              <KunIcon
                :name="entry.icon"
                class="text-primary-500 mt-0.5 size-5 shrink-0"
              />
              <span class="min-w-0">
                <span class="block font-medium">{{ entry.title }}</span>
                <span class="text-default-500 mt-1 block text-xs leading-5">
                  {{ entry.description }}
                </span>
              </span>
            </NuxtLink>
          </li>
        </ul>
      </section>

      <nav
        class="mt-10 grid gap-3 sm:grid-cols-2"
        aria-label="上一篇与下一篇"
      >
        <NuxtLink
          v-if="previous"
          :to="`/guidelines/${previous.slug}`"
          class="border-default-200 hover:border-primary-300 rounded-xl border p-4 transition-colors"
        >
          <span class="text-default-500 flex items-center gap-1 text-xs">
            <KunIcon name="lucide:arrow-left" class="size-3.5" />
            上一篇
          </span>
          <span class="mt-1 block font-medium">{{ previous.title }}</span>
        </NuxtLink>
        <span v-else />
        <NuxtLink
          v-if="next"
          :to="`/guidelines/${next.slug}`"
          class="border-default-200 hover:border-primary-300 rounded-xl border p-4 text-right transition-colors"
        >
          <span
            class="text-default-500 flex items-center justify-end gap-1 text-xs"
          >
            下一篇
            <KunIcon name="lucide:arrow-right" class="size-3.5" />
          </span>
          <span class="mt-1 block font-medium">{{ next.title }}</span>
        </NuxtLink>
      </nav>
    </article>

    <aside class="hidden w-56 shrink-0 xl:block">
      <div class="sticky top-20 max-h-[calc(100dvh-6rem)] overflow-y-auto">
        <GuidelinesToc :key="doc.slug" :headings="doc.headings" />
      </div>
    </aside>
  </div>
</template>
