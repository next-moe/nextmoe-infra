<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    tone?: 'danger' | 'success' | 'info' | 'warning'
  }>(),
  { tone: 'danger' }
)

const TONE = {
  danger: { box: 'bg-danger-50 text-danger', icon: 'lucide:circle-alert' },
  success: { box: 'bg-success-50 text-success', icon: 'lucide:circle-check' },
  info: { box: 'bg-primary-50 text-primary', icon: 'lucide:info' },
  warning: { box: 'bg-warning-50 text-warning', icon: 'lucide:triangle-alert' }
} as const

const style = computed(() => TONE[props.tone])
</script>

<template>
  <div
    class="flex items-start gap-2.5 rounded-xl px-3.5 py-3 text-sm"
    :class="style.box"
  >
    <KunIcon :name="style.icon" class="mt-0.5 size-4 shrink-0" />
    <span class="min-w-0 leading-relaxed"><slot /></span>
  </div>
</template>
