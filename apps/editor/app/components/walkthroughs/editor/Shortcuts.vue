<script setup lang="ts">
import { WT_SHORTCUTS } from '~/constants/walkthrough'

const open = defineModel<boolean>({ default: false })
</script>

<template>
  <KunModal
    v-model="open"
    title="攻略编辑器快捷键"
    description="在步骤的输入框里就能完成大部分操作，不用离开键盘。"
  >
    <dl class="divide-default-100 divide-y">
      <div
        v-for="shortcut in WT_SHORTCUTS"
        :key="shortcut.keys"
        class="flex items-center justify-between gap-4 py-2 text-sm"
      >
        <dt class="text-default-600">{{ shortcut.label }}</dt>
        <dd><KunKbd :keys="shortcut.keys" /></dd>
      </div>
    </dl>
  </KunModal>
</template>
