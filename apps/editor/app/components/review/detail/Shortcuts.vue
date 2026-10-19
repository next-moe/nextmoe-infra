<script setup lang="ts">
const SHORTCUTS = [
  ['j / k', '下一个 / 上一个字段'],
  ['a', '采纳当前字段'],
  ['e', '修正当前字段'],
  ['r', '拒绝当前字段'],
  ['m', '合并提案'],
  ['d', '拒绝整条提案'],
  ['] / [', '下一条 / 上一条提案'],
  ['?', '显示或隐藏快捷键']
] as const
</script>

<template>
  <KunCard bordered padding="sm">
    <ul class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2 lg:grid-cols-4">
      <li
        v-for="[keys, label] in SHORTCUTS"
        :key="keys"
        class="flex items-center gap-2"
      >
        <kbd
          class="bg-default-100 text-default-700 rounded px-1.5 py-0.5 font-mono text-xs"
          >{{ keys }}</kbd
        >
        <span class="text-default-500">{{ label }}</span>
      </li>
    </ul>
  </KunCard>
</template>
