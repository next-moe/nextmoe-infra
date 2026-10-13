<script setup lang="ts">
import type {
  ExploreEntityTarget,
  ExploreWorkCharacter
} from '~~/shared/types/explore'

const props = defineProps<{ character: ExploreWorkCharacter; nsfw: boolean }>()
const emit = defineEmits<{ open: [target: ExploreEntityTarget] }>()

const image = computed(() =>
  props.character.image && isImageShown(props.character.image, props.nsfw)
    ? props.character.image
    : null
)
const zhName = computed(() => {
  const zh = zhLocalized(props.character.localized)
  return zh && zh.value !== props.character.display_name ? zh.value : null
})
const openCharacter = () =>
  emit('open', { kind: 'characters', id: props.character.id })
</script>

<template>
  <div
    class="group border-default-200 bg-content1 hover:border-primary overflow-hidden rounded-xl border transition-colors"
  >
    <button
      type="button"
      class="block w-full"
      :aria-label="character.display_name"
      @click="openCharacter"
    >
      <div class="bg-default-100 aspect-[3/4] overflow-hidden">
        <KunImageNative
          v-if="image"
          :src="image.url"
          :alt="character.display_name"
          loading="lazy"
          class-name="h-full w-full object-cover object-top transition-transform duration-300 group-hover:scale-[1.03]"
        />
        <div
          v-else
          class="text-default-300 flex h-full items-center justify-center"
        >
          <KunIcon name="lucide:user-round" class="size-10" />
        </div>
      </div>
    </button>

    <div class="space-y-1 p-3">
      <div class="flex items-start gap-1.5">
        <button
          type="button"
          class="text-foreground hover:text-primary min-w-0 flex-1 truncate text-left text-sm font-semibold"
          @click="openCharacter"
        >
          {{ character.display_name }}
        </button>
        <KunChip
          v-if="character.spoiler && character.spoiler !== 'none'"
          color="warning"
          variant="flat"
          size="xs"
        >
          剧透
        </KunChip>
      </div>
      <p v-if="zhName" class="text-default-500 truncate text-xs">
        {{ zhName }}
      </p>
      <p
        v-if="character.voices?.length"
        class="text-default-400 flex flex-wrap items-center gap-x-1.5 text-xs"
      >
        <KunIcon name="lucide:mic" class="size-3" />
        <button
          v-for="v in character.voices"
          :key="v.id"
          type="button"
          class="hover:text-primary hover:underline"
          @click="emit('open', { kind: 'credit-names', id: v.id })"
        >
          {{ v.display_name }}
        </button>
      </p>
    </div>
  </div>
</template>
