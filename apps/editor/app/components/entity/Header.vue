<script setup lang="ts">
import type { CatalogEntitySummary } from '#shared/types/catalog'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const props = defineProps<{
  family: EditFamily
  entityId: string
  summary: CatalogEntitySummary | null
  mode: 'view' | 'edit'
}>()

const meta = computed(() => ENTITY_META[props.family])
const name = computed(
  () =>
    props.summary?.display_name ||
    props.summary?.title ||
    `${meta.value.label} #${props.entityId}`
)
const picture = computed(
  () =>
    props.summary?.image ?? props.summary?.photo ?? props.summary?.logo ?? null
)
</script>

<template>
  <div class="space-y-3">
    <NuxtLink
      :to="meta.path"
      class="text-default-500 hover:text-foreground inline-flex items-center gap-1 text-sm"
    >
      <KunIcon name="lucide:chevron-left" class="size-4" />
      {{ meta.label }}
    </NuxtLink>
    <header class="flex items-center gap-4">
      <div
        class="bg-default-100 flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-lg sm:size-20"
      >
        <KunImage
          v-if="picture?.url"
          :src="picture.url"
          :thumbhash="picture.thumbhash ?? undefined"
          :alt="name"
          aspect-ratio="1 / 1"
          object-fit="cover"
          loading="eager"
        />
        <KunIcon v-else :name="meta.icon" class="text-default-300 size-7" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="text-default-500 flex items-center gap-1 text-xs">
          <KunIcon
            :name="props.mode === 'edit' ? 'lucide:pencil' : meta.icon"
            class="size-3.5"
          />
          {{ props.mode === 'edit' ? `编辑${meta.label}` : meta.label }}
        </p>
        <h1 class="truncate text-xl font-bold sm:text-2xl">{{ name }}</h1>
        <p
          class="text-default-500 mt-0.5 flex flex-wrap items-center gap-2 text-sm"
        >
          <span v-if="props.summary?.latin" class="truncate">
            {{ props.summary.latin }}
          </span>
          <span class="inline-flex items-center gap-1">
            条目 ID
            <KunCopy :text="props.entityId" />
          </span>
        </p>
      </div>
      <div class="flex shrink-0 flex-wrap justify-end gap-2">
        <KunButton
          :href="`/history/${props.family}/${props.entityId}`"
          variant="light"
          size="sm"
        >
          <KunIcon name="lucide:history" class="size-4" />
          修订历史
        </KunButton>
        <KunButton
          v-if="props.mode === 'view'"
          :href="`${meta.path}/${props.entityId}/edit`"
          color="primary"
          size="sm"
        >
          <KunIcon name="lucide:pencil" class="size-4" />
          编辑
        </KunButton>
        <KunButton
          v-else
          :href="`${meta.path}/${props.entityId}`"
          variant="bordered"
          size="sm"
        >
          <KunIcon name="lucide:eye" class="size-4" />
          查看资料
        </KunButton>
      </div>
    </header>
  </div>
</template>
