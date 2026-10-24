<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import {
  ATTRIBUTION_ROLE_LABELS,
  LANGUAGE_LABELS,
  TITLE_KIND_LABELS
} from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const preferences = useEditorPreferences()

const titles = computed(() => props.work.titles ?? [])
const companies = computed(() => props.work.companies ?? [])
const platforms = computed(() => [
  ...new Set((props.work.platforms ?? []).map((p) => p.platform))
])
</script>

<template>
  <div class="grid gap-4 lg:grid-cols-3">
    <WorksDetailSection
      title="标题"
      :count="titles.length"
      class="lg:col-span-2"
    >
      <table v-if="titles.length" class="w-full text-sm">
        <thead class="text-default-400 text-left text-xs">
          <tr>
            <th class="pb-2 font-normal">标题</th>
            <th class="pb-2 font-normal">语言</th>
            <th class="pb-2 font-normal">类型</th>
          </tr>
        </thead>
        <tbody class="divide-default-200 divide-y">
          <tr
            v-for="(title, index) in titles"
            :key="`${title.lang}-${title.title}-${index}`"
          >
            <td class="py-2 pr-3">
              <p class="break-words">{{ title.title }}</p>
              <p v-if="title.latin" class="text-default-400 text-xs">
                {{ title.latin }}
              </p>
            </td>
            <td class="text-default-500 py-2 pr-3 whitespace-nowrap">
              {{ labelOf(LANGUAGE_LABELS, title.lang) || '—' }}
            </td>
            <td class="py-2 whitespace-nowrap">
              <span class="text-default-500">{{
                labelOf(TITLE_KIND_LABELS, title.title_kind)
              }}</span>
              <KunChip
                v-if="title.is_machine"
                size="xs"
                variant="flat"
                color="warning"
                class-name="ml-1"
              >
                机翻
              </KunChip>
            </td>
          </tr>
        </tbody>
      </table>
      <KunNull v-else description="暂无标题" :is-show-sticker="false" />
    </WorksDetailSection>

    <div class="space-y-4">
      <WorksDetailSection title="厂商" :count="companies.length">
        <ul v-if="companies.length" class="space-y-2 text-sm">
          <li
            v-for="company in companies"
            :key="`${company.id}-${company.attribution_role}`"
            class="flex items-center justify-between gap-2"
          >
            <NuxtLink
              :to="`/companies/${company.id}`"
              class="hover:text-primary min-w-0 truncate"
            >
              {{ preferences.nameOf(company) }}
            </NuxtLink>
            <span class="text-default-400 shrink-0 text-xs">
              {{ labelOf(ATTRIBUTION_ROLE_LABELS, company.attribution_role) }}
            </span>
          </li>
        </ul>
        <KunNull v-else description="暂无厂商" :is-show-sticker="false" />
      </WorksDetailSection>

      <WorksDetailSection title="引擎与平台">
        <div class="space-y-2 text-sm">
          <div class="flex flex-wrap gap-1.5">
            <NuxtLink
              v-for="engine in props.work.engines ?? []"
              :key="engine.id"
              :to="`/engines/${engine.id}`"
            >
              <KunChip size="sm" variant="flat" color="info">{{
                engine.display_name
              }}</KunChip>
            </NuxtLink>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <KunChip
              v-for="platform in platforms"
              :key="platform"
              size="sm"
              variant="flat"
            >
              {{ platform }}
            </KunChip>
          </div>
          <p
            v-if="!(props.work.engines ?? []).length && !platforms.length"
            class="text-default-400"
          >
            暂无记录
          </p>
        </div>
      </WorksDetailSection>

      <WorksDetailSection
        title="系列"
        :count="(props.work.series ?? []).length"
      >
        <ul v-if="(props.work.series ?? []).length" class="space-y-1 text-sm">
          <li v-for="series in props.work.series" :key="series.id">
            <NuxtLink :to="`/series/${series.id}`" class="hover:text-primary">
              {{ series.display_name }}
            </NuxtLink>
            <span class="text-default-400 ml-1 text-xs"
              >{{ series.member_count }} 部</span
            >
          </li>
        </ul>
        <KunNull v-else description="不属于任何系列" :is-show-sticker="false" />
      </WorksDetailSection>
    </div>
  </div>
</template>
