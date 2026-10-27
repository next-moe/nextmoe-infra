<script setup lang="ts">
import { LANDING_APIS } from '~/constants/landing'
</script>

<template>
  <div>
    <h2 class="text-foreground text-2xl font-bold tracking-tight">主要 API</h2>
    <p class="text-default-500 mt-3 max-w-2xl text-sm leading-relaxed">
      目录数据是地基，补丁、下载资源、表情包与分销都按同一个作品 id 挂在它上面。
    </p>

    <div class="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <NuxtLink
        v-for="api in LANDING_APIS"
        :key="api.prefix"
        :to="api.to"
        class="group border-default-200 bg-content1 hover:border-primary flex flex-col rounded-xl border p-5 transition-colors"
      >
        <div class="flex items-center gap-3">
          <div
            class="bg-default-100 text-foreground flex size-10 shrink-0 items-center justify-center rounded-lg"
          >
            <KunIcon :name="api.icon" class="size-5" />
          </div>
          <div class="min-w-0">
            <h3 class="text-foreground text-base font-semibold">
              {{ api.title }}
            </h3>
            <p class="text-default-400 truncate font-mono text-xs">
              {{ api.prefix }}
            </p>
          </div>
        </div>
        <p class="text-default-500 mt-3 flex-1 text-sm leading-relaxed">
          {{ api.body }}
        </p>
        <span
          class="text-primary mt-4 inline-flex items-center gap-1 text-sm font-medium"
        >
          查看文档
          <KunIcon
            name="lucide:arrow-right"
            class="size-4 transition-transform group-hover:translate-x-0.5"
          />
        </span>
      </NuxtLink>
    </div>
  </div>
</template>
