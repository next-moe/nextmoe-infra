<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = defineProps<{
  decoration: ShopDecoration | null | undefined
  className?: string
}>()

const reduceMotion = ref(true)
let mq: MediaQueryList | null = null
const sync = () => (reduceMotion.value = mq?.matches ?? true)
onMounted(() => {
  mq = window.matchMedia('(prefers-reduced-motion: reduce)')
  sync()
  mq.addEventListener('change', sync)
})
onBeforeUnmount(() => mq?.removeEventListener('change', sync))

const src = computed(() => {
  const d = props.decoration
  if (!d) return ''
  return !reduceMotion.value && d.animated_url ? d.animated_url : d.static_url
})
</script>

<template>
  <div
    :class="cn('bg-default-100 aspect-[3/1] w-full overflow-hidden', className)"
  >
    <KunImage
      v-if="src"
      :src="src"
      alt=""
      class-name="size-full"
      object-fit="cover"
    />
  </div>
</template>
