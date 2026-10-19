<script setup lang="ts">
import type { CatalogWork } from '#shared/types/catalog'
import { ROSTER_ROLE_LABELS } from '~/constants/catalog'

const props = defineProps<{ work: CatalogWork }>()

const credits = computed(() => props.work.credits ?? [])
const characters = computed(() => props.work.characters ?? [])
</script>

<template>
  <div class="grid gap-4 lg:grid-cols-2">
    <WorksDetailSection
      title="制作人员"
      :count="credits.reduce((n, g) => n + g.credits.length, 0)"
    >
      <dl v-if="credits.length" class="space-y-3 text-sm">
        <div v-for="group in credits" :key="group.role_key">
          <dt class="text-default-400 mb-1 text-xs">{{ group.role_name }}</dt>
          <dd class="flex flex-wrap gap-1.5">
            <NuxtLink
              v-for="entry in group.credits"
              :key="`${entry.id}-${entry.character_id ?? 0}`"
              :to="`/credit-names/${entry.id}`"
            >
              <KunChip size="sm" variant="flat">{{
                entry.display_name
              }}</KunChip>
            </NuxtLink>
          </dd>
        </div>
      </dl>
      <KunNull v-else description="暂无制作人员" :is-show-sticker="false" />
    </WorksDetailSection>

    <WorksDetailSection title="登场角色" :count="characters.length">
      <ul v-if="characters.length" class="divide-default-200 divide-y text-sm">
        <li
          v-for="character in characters"
          :key="character.id"
          class="flex items-center gap-3 py-2"
        >
          <div
            class="bg-default-100 size-10 shrink-0 overflow-hidden rounded-full"
          >
            <KunImage
              v-if="character.image?.url"
              :src="character.image.url"
              alt=""
              aspect-ratio="1 / 1"
              object-fit="cover"
            />
          </div>
          <div class="min-w-0 flex-1">
            <NuxtLink
              :to="`/characters/${character.id}`"
              class="hover:text-primary block truncate font-medium"
            >
              {{ character.display_name }}
            </NuxtLink>
            <p class="text-default-400 truncate text-xs">
              {{ labelOf(ROSTER_ROLE_LABELS, character.roster_role) }}
              <template v-if="character.voices?.length">
                · CV
                {{ character.voices.map((v) => v.display_name).join('、') }}
              </template>
            </p>
          </div>
        </li>
      </ul>
      <KunNull v-else description="暂无角色" :is-show-sticker="false" />
    </WorksDetailSection>
  </div>
</template>
