<script setup lang="ts">
import type { DuplicateSuspect } from '~/utils/creation'

const props = defineProps<{
  suspects: DuplicateSuspect[]
  path: string
  submitting?: boolean
}>()

const emit = defineEmits<{ confirm: [] }>()

const open = defineModel<boolean>({ required: true })
</script>

<template>
  <KunModal
    v-model="open"
    title="可能已经有这个条目了"
    description="资料库里已有名字相同的条目。请先确认它们是不是你要新建的那一个；确实不同时再继续创建。"
    size="lg"
    role="alertdialog"
  >
    <div class="space-y-4">
      <ul class="divide-default-200 divide-y">
        <li
          v-for="suspect in props.suspects"
          :key="suspect.id"
          class="flex items-center gap-2 py-2"
        >
          <NuxtLink
            :to="`${props.path}/${suspect.id}`"
            target="_blank"
            class="hover:text-primary min-w-0 flex-1 truncate text-sm"
          >
            {{ suspect.display_name }}
          </NuxtLink>
          <span class="text-default-400 font-mono text-xs"
            >#{{ suspect.id }}</span
          >
        </li>
      </ul>
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">返回修改</KunButton>
        <KunButton
          color="warning"
          :loading="props.submitting"
          @click="emit('confirm')"
        >
          仍要创建
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
