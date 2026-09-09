<script setup lang="ts">
const MASCOT_SRC = '/mascot/auth.webp'

const hasArt = ref(true)

const hideArt = () => {
  hasArt.value = false
}
</script>

<template>
  <div class="relative flex h-52 items-end justify-center">
    <span
      class="bg-primary-100 absolute bottom-0 left-1/2 h-12 w-56 -translate-x-1/2 rounded-full"
      aria-hidden="true"
    />
    <span
      class="border-primary-200 absolute bottom-10 left-1/2 size-32 -translate-x-1/2 rounded-full border-2"
      aria-hidden="true"
    />
    <span
      class="bg-primary-200 absolute bottom-20 left-2 size-10 rounded-full"
      aria-hidden="true"
    />
    <span
      class="bg-primary-200 absolute right-4 bottom-8 size-5 rounded-full"
      aria-hidden="true"
    />

    <ClientOnly>
      <img
        v-if="hasArt"
        :src="MASCOT_SRC"
        alt=""
        class="relative max-h-52 w-auto object-contain"
        @error="hideArt"
      >
    </ClientOnly>
  </div>
</template>
