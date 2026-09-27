<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'
import { cn } from '@kungal/ui-core'

interface NavItem {
  key: string
  label: string
  hint?: string
  icon: string
  count: number
  to: RouteLocationRaw
}

const props = defineProps<{
  sections: { title: string; items: NavItem[] }[]
  active: string
}>()

const items = computed(() => props.sections.flatMap((s) => s.items))

const selectOptions = computed(() =>
  props.sections.flatMap((s) =>
    s.items.map((item) => ({
      value: item.key,
      label: `${s.title ? `${s.title} · ` : ''}${item.label}（${item.count}）`
    }))
  )
)

const onSelect = (key: string | number | (string | number)[] | null) => {
  const item = items.value.find((i) => i.key === key)
  if (item) navigateTo(item.to)
}
</script>

<template>
  <div>
    <KunSelect
      :model-value="active || null"
      :options="selectOptions"
      placeholder="选择视图"
      aria-label="选择视图"
      class-name="lg:hidden"
      @update:model-value="onSelect"
    />

    <nav
      class="sticky top-4 hidden max-h-[calc(100dvh-6rem)] space-y-4 overflow-y-auto pr-1 lg:block"
      aria-label="客户端视图"
    >
      <div v-for="section in sections" :key="section.title" class="space-y-1">
        <p
          v-if="section.title"
          class="text-default-400 px-3 pb-1 text-xs font-medium tracking-wide"
        >
          {{ section.title }}
        </p>
        <NuxtLink
          v-for="item in section.items"
          :key="item.key"
          :to="item.to"
          :title="item.hint"
          :class="
            cn(
              'flex items-center gap-2 rounded-lg px-3 py-1.5 transition-colors',
              item.key === active
                ? 'bg-primary-50 text-primary'
                : 'text-default-600 hover:bg-primary-50 hover:text-primary',
              item.count === 0 && item.key !== active && 'text-default-400'
            )
          "
        >
          <KunIcon :name="item.icon" class="size-4 shrink-0" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm">{{ item.label }}</span>
            <span
              v-if="item.hint"
              class="text-default-400 block truncate text-xs"
            >
              {{ item.hint }}
            </span>
          </span>
          <span class="text-default-400 shrink-0 text-xs tabular-nums">{{
            item.count
          }}</span>
        </NuxtLink>
      </div>
    </nav>
  </div>
</template>
