<script setup lang="ts">
import { findNavItem } from '~/constants/navigation'

const route = useRoute()

const item = computed(() => findNavItem(route.path))

useHead(() => ({ title: `${item.value?.label ?? '页面不存在'} · 未萌编辑台` }))
</script>

<template>
  <div
    class="mx-auto flex max-w-xl flex-col items-center gap-4 py-16 text-center"
  >
    <template v-if="item">
      <span
        class="bg-primary-50 text-primary flex size-14 items-center justify-center rounded-2xl"
      >
        <KunIcon :name="item.icon" class="size-7" />
      </span>
      <div class="space-y-1">
        <h1 class="text-xl font-semibold">{{ item.label }} · 即将推出</h1>
        <p class="text-default-500 text-sm">
          这一部分正在建设中。编辑台会分批开放各类条目的编辑，作品已经可以浏览。
        </p>
      </div>
      <KunButton variant="bordered" @click="navigateTo('/works')"
        >去看看作品</KunButton
      >
    </template>
    <template v-else>
      <KunNull description="没有找到这个页面" />
      <KunButton variant="bordered" @click="navigateTo('/')"
        >回到工作台</KunButton
      >
    </template>
  </div>
</template>
