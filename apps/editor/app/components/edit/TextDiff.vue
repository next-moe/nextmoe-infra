<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = defineProps<{ before: string; after: string }>()

const lines = computed(() => diffLines(props.before, props.after))
const left = computed(() => lines.value.filter((line) => line.type !== 'add'))
const right = computed(() => lines.value.filter((line) => line.type !== 'del'))
</script>

<template>
  <div class="grid gap-2 text-xs leading-5 md:grid-cols-2">
    <div class="border-default-200 overflow-hidden rounded-lg border">
      <p class="bg-default-50 text-default-500 px-2 py-1">修改前</p>
      <div class="max-h-80 overflow-auto p-2">
        <p
          v-for="(line, i) in left"
          :key="i"
          :class="
            cn(
              'min-h-5 px-1 break-words whitespace-pre-wrap',
              line.type === 'del' && 'bg-danger-50 text-danger-700 line-through'
            )
          "
        >
          {{ line.text }}
        </p>
        <p v-if="!left.length" class="text-default-400">（空）</p>
      </div>
    </div>
    <div class="border-default-200 overflow-hidden rounded-lg border">
      <p class="bg-default-50 text-default-500 px-2 py-1">修改后</p>
      <div class="max-h-80 overflow-auto p-2">
        <p
          v-for="(line, i) in right"
          :key="i"
          :class="
            cn(
              'min-h-5 px-1 break-words whitespace-pre-wrap',
              line.type === 'add' && 'bg-success-50 text-success-700'
            )
          "
        >
          {{ line.text }}
        </p>
        <p v-if="!right.length" class="text-default-400">（空）</p>
      </div>
    </div>
  </div>
</template>
