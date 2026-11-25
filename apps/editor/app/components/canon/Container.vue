<script setup lang="ts">
import type { CanonPage, CanonWorkHit } from '#shared/types/canon'
import { CANON_CREATE_ROOTS, CANON_CREATE_TEMPLATES } from '~/constants/canonCreate'
import { canonMessage } from '~/utils/canon/record'

useHead({ title: '自有目录 · 未萌编辑台' })

const api = useEditorApi()
const query = ref('')
const hits = ref<CanonWorkHit[]>([])
const searched = ref(false)
const loading = ref(false)

const search = async () => {
  const q = query.value.trim()
  if (!q) {
    return
  }
  loading.value = true
  try {
    const page = await api<CanonPage<CanonWorkHit>>('/v3/catalog/search', {
      query: { q, limit: 30 }
    })
    hits.value = page.items
    searched.value = true
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunHeader
      name="自有目录"
      description="自有目录（canon）里的作品。改动直接生效，写入的字段采集程序不能覆盖，只能提议。"
    />
    <div class="flex flex-wrap gap-2">
      <KunButton
        v-for="key in CANON_CREATE_ROOTS"
        :key="key"
        variant="bordered"
        size="sm"
        @click="navigateTo(`/canon/new?kind=${key}`)"
      >
        {{ CANON_CREATE_TEMPLATES[key]!.label }}
      </KunButton>
    </div>
    <form class="flex gap-2" @submit.prevent="search">
      <KunInput
        v-model="query"
        placeholder="作品标题"
        aria-label="作品标题"
        class-name="flex-1"
      />
      <KunButton color="primary" type="submit" :loading="loading">搜索</KunButton>
    </form>
    <KunCard v-if="hits.length" bordered padding="none">
      <ul class="divide-default-200 divide-y">
        <li v-for="w in hits" :key="w.id">
          <NuxtLink
            :to="`/canon/work/${w.id}`"
            class="hover:bg-default-100 flex items-center gap-3 px-4 py-3"
          >
            <span class="text-foreground min-w-0 flex-1 truncate">
              {{ w.titles[0]?.text }}
            </span>
            <KunChip size="sm" variant="flat">{{ w.medium }}</KunChip>
            <span class="text-default-400 font-mono text-xs">#{{ w.id }}</span>
          </NuxtLink>
        </li>
      </ul>
    </KunCard>
    <KunNull v-else-if="searched" description="没有这个标题的作品" />
  </div>
</template>
