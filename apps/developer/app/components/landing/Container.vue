<script setup lang="ts">
import type { CatalogStats } from '~~/shared/types/stats'

const auth = useAuth()

useSeoMeta({
  title: '开发者平台',
  description:
    'NextMoe 开发者平台 — ACGN 数据，以此为准。同一部作品在 VNDB、Bangumi、DLsite、ErogameScape、Ci-en、Getchu 六个源各有一个页面，NextMoe 把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上答案取自哪个源。不用注册、不带密钥就能调用，完全免费。',
  ogTitle: 'NextMoe 开发者平台',
  ogDescription: 'ACGN 数据，以此为准 — one canon, every source reconciled.',
  ogImage: '/android-chrome-512x512.png'
})

interface V2Stats {
  object?: string
  works?: number
  companies?: number
  characters?: number
  credit_names?: number
  persons?: number
}

const { data: stats } = await useFetch<CatalogStats | V2Stats>('/relay/stats', {
  key: 'landing-catalog-stats',
  timeout: 5000
})

const counts = computed(() => {
  const s = stats.value as (CatalogStats & V2Stats) | null
  if (!s) return null
  if (
    'works' in s &&
    typeof s.works === 'object' &&
    s.works &&
    'total' in s.works
  ) {
    return s as CatalogStats
  }
  if (typeof s.works === 'number') {
    return {
      works: { total: s.works, by_medium: [] },
      entities: {
        labels: s.companies ?? 0,
        characters: s.characters ?? 0,
        credit_names: s.credit_names ?? 0,
        persons: s.persons ?? 0
      }
    } satisfies CatalogStats
  }
  return null
})

const formatCount = (n: number): string => {
  if (n < 10000) return String(n)
  const wan = n / 10000
  return `${wan >= 1000 ? Math.round(wan) : Number(wan.toFixed(1))} 万`
}

const metrics = computed(() => {
  const c = counts.value
  if (!c) return []
  return [
    { value: formatCount(c.works.total), label: '作品' },
    { value: formatCount(c.entities.characters), label: '角色' },
    { value: formatCount(c.entities.labels), label: '厂牌 · 社团' },
    { value: formatCount(c.entities.credit_names), label: '制作人员名义' },
    { value: formatCount(c.entities.persons), label: '人物' }
  ]
})

const galgameCount = computed(() => {
  const row = counts.value?.works.by_medium.find((m) => m.medium === 'galgame')
  return row ? formatCount(row.count) : ''
})
</script>

<template>
  <div>
    <section
      class="border-default-200 bg-content1 relative isolate overflow-hidden border-b"
    >
      <div class="kun-hero-glow pointer-events-none absolute inset-0 -z-10" />

      <div
        class="mx-auto grid max-w-7xl items-end px-4 py-12 md:px-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,30rem)] lg:gap-12"
      >
        <div
          class="pt-14 pb-10 text-center lg:self-center lg:py-24 lg:text-left"
        >
          <div
            class="border-default-200 bg-background text-default-500 inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium"
          >
            <span class="bg-success size-2 rounded-full" />
            免注册调用 · 六源对齐 · 完全免费
          </div>

          <h1
            class="text-foreground mt-7 text-[2.75rem] leading-[1.06] font-bold tracking-tight sm:text-6xl xl:text-7xl 2xl:text-[5rem]"
          >
            ACGN 数据，<br />
            以此为准
          </h1>
          <p
            class="text-primary mt-5 font-mono text-sm tracking-wide lg:text-base"
          >
            One canon. Every source reconciled.
          </p>
          <p
            class="text-default-500 mx-auto mt-6 max-w-xl text-base leading-relaxed lg:mx-0 lg:text-lg"
          >
            同一部作品在六个上游各有一个页面，写的还常常不一样。NextMoe
            把它们对齐成一条记录，逐字段给出裁定后的标准答案，并附上这个答案取自哪个源。
          </p>
          <p
            class="text-foreground mx-auto mt-4 max-w-xl text-base leading-relaxed font-medium lg:mx-0 lg:text-lg"
          >
            不用注册，不带密钥，一条 curl 就能拿到。
          </p>

          <div
            class="mt-9 flex flex-wrap items-center justify-center gap-3 lg:justify-start"
          >
            <KunButton
              v-if="auth.isLoggedIn.value"
              color="primary"
              size="lg"
              @click="navigateTo('/dashboard')"
            >
              进入控制台
              <KunIcon name="lucide:arrow-right" class="ml-1 size-4" />
            </KunButton>
            <KunButton
              v-else
              color="primary"
              size="lg"
              @click="navigateTo('/explore')"
            >
              免注册，直接试
              <KunIcon name="lucide:arrow-right" class="ml-1 size-4" />
            </KunButton>
            <KunButton variant="flat" size="lg" @click="navigateTo('/docs')">
              <KunIcon name="lucide:book-open" class="mr-1 size-4" />
              查看 API 文档
            </KunButton>
          </div>
        </div>

        <div class="flex justify-center self-end lg:justify-end">
          <img
            src="/koi.webp"
            alt="NextMoe 看板娘 恋（Koi）向你伸出手"
            width="608"
            height="1080"
            fetchpriority="high"
            class="kun-koi block h-[20rem] w-auto object-contain sm:h-[26rem] lg:h-[38rem] xl:h-[44rem]"
          />
        </div>
      </div>
    </section>

    <section v-if="metrics.length" class="border-default-200 border-b">
      <div
        class="mx-auto grid max-w-7xl grid-cols-2 gap-y-8 px-4 py-12 md:grid-cols-5 md:px-6"
      >
        <div
          v-for="metric in metrics"
          :key="metric.label"
          class="text-center last:col-span-2 md:text-left md:last:col-span-1"
        >
          <p
            class="text-foreground text-3xl font-bold tracking-tight tabular-nums md:text-4xl"
          >
            {{ metric.value }}
          </p>
          <p class="text-default-400 mt-1.5 text-xs">{{ metric.label }}</p>
        </div>
      </div>
    </section>

    <section class="border-default-200 border-b">
      <div class="mx-auto max-w-7xl px-4 py-16 md:px-6 md:py-20">
        <LandingApis />
      </div>
    </section>

    <section class="mx-auto max-w-7xl px-4 py-16 md:px-6 md:py-20">
      <LandingTryIt />

      <p v-if="metrics.length" class="text-default-400 mt-10 text-xs">
        上方数字取自
        <code class="text-default-500 font-mono">/v2/catalog/stats</code>
        <template v-if="galgameCount"
          >，其中 Galgame {{ galgameCount }} 部</template
        >
        ，每 10 分钟刷新一次。
      </p>
    </section>

    <section class="border-default-200 border-t">
      <div class="mx-auto max-w-7xl px-4 py-16 md:px-6 md:py-20">
        <LandingTiers />
      </div>
    </section>
  </div>
</template>
