<script setup lang="ts">
import { GUIDELINE_DOCS } from '~/generated/guidelines'

const query = ref('')
const hits = computed(() => searchGuidelines(GUIDELINE_DOCS, query.value))
</script>

<template>
  <div>
    <KunInput
      v-model="query"
      placeholder="搜索编辑规范，例如：署名、剧透、配额"
      aria-label="搜索编辑规范"
    >
      <template #prefix>
        <KunIcon name="lucide:search" class="text-default-400 size-4" />
      </template>
    </KunInput>
    <ul
      v-if="query.trim()"
      class="border-default-200 bg-content1 divide-default-100 mt-2 divide-y rounded-xl border"
    >
      <li v-for="hit in hits" :key="`${hit.doc.slug}#${hit.anchor ?? ''}`">
        <NuxtLink
          :to="guidelineHref(hit)"
          class="hover:bg-default-50 flex items-start gap-3 px-4 py-3"
        >
          <KunIcon
            :name="hit.doc.icon"
            class="text-default-400 mt-0.5 size-4 shrink-0"
          />
          <span class="min-w-0">
            <span class="block text-sm font-medium">{{ hit.label }}</span>
            <span class="text-default-500 mt-0.5 block text-xs leading-5">
              {{ hit.snippet }}
            </span>
          </span>
        </NuxtLink>
      </li>
      <li
        v-if="!hits.length"
        class="text-default-500 px-4 py-6 text-center text-sm"
      >
        没有找到相关内容，换个词试试。
      </li>
    </ul>
  </div>
</template>
