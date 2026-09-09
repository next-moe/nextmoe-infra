<script setup lang="ts">
import type { KunCommandItem } from '@kungal/ui-vue'
import type { SearchEntry, SearchHeading } from '~~/shared/types/search'

interface Hit {
  entry: SearchEntry
  score: number
  heading?: SearchHeading
  snippet?: string
}

const open = ref(false)
const query = ref('')
const loading = ref(false)
const index = shallowRef<SearchEntry[]>([])

// The index is ~170 KB of prose and every page on the site is in it, so it must
// never reach the initial bundle: this dynamic import is its own chunk, fetched
// the first time someone opens the palette and kept afterwards.
const loadIndex = async () => {
  if (index.value.length || loading.value) return
  loading.value = true
  try {
    index.value = (await import('~/generated/search-index')).searchIndex
  } finally {
    loading.value = false
  }
}

watch(open, (isOpen) => {
  if (isOpen) loadIndex()
})

const terms = computed(() =>
  query.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
)

const count = (haystack: string, term: string, cap: number) => {
  let n = 0
  let i = haystack.indexOf(term)
  while (i !== -1 && n < cap) {
    n++
    i = haystack.indexOf(term, i + term.length)
  }
  return n
}

const snippetAround = (body: string, ts: string[]) => {
  const lower = body.toLowerCase()
  let at = -1
  for (const term of ts) {
    const i = lower.indexOf(term)
    if (i !== -1 && (at === -1 || i < at)) at = i
  }
  if (at === -1) return undefined
  const start = Math.max(0, at - 24)
  const end = Math.min(body.length, at + 76)
  return `${start > 0 ? '…' : ''}${body.slice(start, end).trim()}${end < body.length ? '…' : ''}`
}

const hits = computed<Hit[]>(() => {
  const ts = terms.value
  if (!ts.length) return []
  const found: Hit[] = []
  for (const entry of index.value) {
    const title = entry.t.toLowerCase()
    const section = entry.s.toLowerCase()
    const subtitle = entry.d.toLowerCase()
    const body = entry.b.toLowerCase()
    let score = 0
    let heading: SearchHeading | undefined
    for (const term of ts) {
      if (title.includes(term)) score += 12
      if (entry.r.toLowerCase().includes(term)) score += 8
      if (subtitle.includes(term)) score += 4
      if (section.includes(term)) score += 2
      for (const h of entry.h ?? []) {
        if (h.t.toLowerCase().includes(term)) {
          score += 6
          if (!heading) heading = h
        }
      }
      score += count(body, term, 5)
    }
    // AND semantics: two terms are a narrowing, not a widening. Without this a
    // second word only ever adds hits, which is the opposite of what typing it
    // means.
    const everywhere = ts.every(
      (term) =>
        title.includes(term) ||
        section.includes(term) ||
        subtitle.includes(term) ||
        body.includes(term) ||
        entry.r.toLowerCase().includes(term)
    )
    if (score > 0 && everywhere) {
      found.push({ entry, score, heading, snippet: snippetAround(entry.b, ts) })
    }
  }
  found.sort((a, b) => b.score - a.score)
  return found.slice(0, 12)
})

const SUGGESTED = [
  '/docs/quickstart',
  '/docs/authentication',
  '/docs/v2',
  '/docs/mcp',
  '/problems',
  '/docs/vocabularies'
]

const iconFor = (route: string) => {
  if (route.startsWith('/problems')) return 'lucide:octagon-alert'
  if (route.startsWith('/docs/vocabularies')) return 'lucide:list-tree'
  if (/^\/docs\/(v2|moyu|sticker)\//.test(route)) return 'lucide:terminal'
  if (route.startsWith('/docs')) return 'lucide:book-open'
  return 'lucide:compass'
}

const toItem = (
  entry: SearchEntry,
  hash?: string,
  description?: string
): KunCommandItem => ({
  value: hash ? `${entry.r}#${hash}` : entry.r,
  label: entry.t,
  description: description || entry.d,
  section: entry.s,
  icon: iconFor(entry.r)
})

const items = computed<KunCommandItem[]>(() => {
  if (!terms.value.length) {
    return SUGGESTED.map((route) =>
      index.value.find((entry) => entry.r === route)
    )
      .filter((entry): entry is SearchEntry => !!entry)
      .map((entry) => toItem(entry))
  }
  return hits.value.map((hit) => toItem(hit.entry, hit.heading?.i, hit.snippet))
})

// No `href` on the items on purpose. KunCommandPalette renders an item with one
// as a real <a> and does not preventDefault on click, so every hit would be a
// full document load instead of a client-side route change.
const go = (item: KunCommandItem) => {
  navigateTo(String(item.value))
}
</script>

<template>
  <KunCommandPalette
    v-model:open="open"
    v-model:query="query"
    :items="items"
    :loading="loading"
    placeholder="搜索文档、端点、错误码、词表…"
    no-result-text="没有匹配的页面"
    empty-text="输入关键字搜索整个开发者平台"
    aria-label="站内搜索"
    @select="go"
  >
    <template #trigger="{ open: openPalette, shortcut }">
      <button
        type="button"
        aria-label="搜索"
        class="border-default-200 text-default-400 hover:border-primary hover:text-default-600 flex cursor-pointer items-center gap-2 rounded-lg border px-2 py-2 text-sm transition-colors md:w-56 md:px-3 md:py-1.5 lg:w-64"
        @click="openPalette"
      >
        <KunIcon name="lucide:search" class="size-4 shrink-0" />
        <span class="hidden flex-1 text-left md:inline">搜索文档…</span>
        <kbd
          class="border-default-200 text-default-400 hidden rounded border px-1.5 py-0.5 text-[0.625rem] font-medium md:inline"
        >
          {{ shortcut }}
        </kbd>
      </button>
    </template>
  </KunCommandPalette>
</template>
