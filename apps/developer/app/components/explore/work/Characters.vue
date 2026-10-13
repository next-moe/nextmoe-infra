<script setup lang="ts">
import { EXPLORE_ROSTER_SECTIONS } from '~/constants/explore'
import type {
  ExploreEntityTarget,
  ExploreWorkCharacter
} from '~~/shared/types/explore'

const props = defineProps<{
  characters: ExploreWorkCharacter[]
  nsfw: boolean
}>()
const emit = defineEmits<{ open: [target: ExploreEntityTarget] }>()

const showSpoilers = ref(false)

const spoilerCount = computed(
  () => props.characters.filter((c) => (c.spoiler ?? 'none') !== 'none').length
)

const sections = computed(() => {
  const visible = props.characters.filter(
    (c) => showSpoilers.value || (c.spoiler ?? 'none') === 'none'
  )
  return EXPLORE_ROSTER_SECTIONS.map((s) => ({
    ...s,
    characters: visible.filter((c) => (c.roster_role ?? 'unknown') === s.role)
  })).filter((s) => s.characters.length)
})
</script>

<template>
  <div class="space-y-8">
    <div
      v-if="spoilerCount"
      class="border-default-200 bg-content1 flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3"
    >
      <p class="text-default-500 text-sm">
        {{ spoilerCount }} 位角色本身带剧透，默认不显示。
      </p>
      <KunSwitch v-model="showSpoilers" size="sm" label="显示剧透角色" />
    </div>

    <section v-for="s in sections" :key="s.role" class="space-y-3">
      <h3
        class="text-foreground flex items-center gap-2 text-base font-semibold"
      >
        {{ s.label }}
        <span class="text-default-400 text-sm font-normal">
          {{ s.characters.length }}
        </span>
      </h3>
      <div
        class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5"
      >
        <ExploreWorkCharacterCard
          v-for="c in s.characters"
          :key="c.id"
          :character="c"
          :nsfw="nsfw"
          @open="emit('open', $event)"
        />
      </div>
    </section>
  </div>
</template>
