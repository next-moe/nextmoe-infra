<script setup lang="ts">
import {
  GUIDELINE_CATEGORIES,
  GUIDELINE_PITFALLS,
  GUIDELINE_PRINCIPLES
} from '~/constants/guidelines'
import { GUIDELINE_DOCS } from '~/generated/guidelines'

const groups = computed(() =>
  GUIDELINE_CATEGORIES.map((category) => ({
    ...category,
    docs: GUIDELINE_DOCS.filter((doc) => doc.category === category.key)
  })).filter((group) => group.docs.length)
)

useHead({ title: '编辑规范 · 未萌编辑台' })
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-10 px-4 py-6 md:px-8 md:py-10">
    <header class="space-y-4">
      <div class="flex items-center gap-3">
        <div
          class="bg-primary-50 text-primary-600 flex size-11 items-center justify-center rounded-xl"
        >
          <KunIcon name="lucide:book-open" class="size-6" />
        </div>
        <div>
          <h1 class="text-2xl font-bold">编辑规范</h1>
          <p class="text-default-500 text-sm">
            编辑资料前需要知道的一切：每一项怎么填、为什么这样填、哪里最容易出错。
          </p>
        </div>
      </div>
      <GuidelinesSearch />
    </header>

    <section class="space-y-4" aria-labelledby="guide-principles">
      <h2 id="guide-principles" class="text-lg font-semibold">基本原则</h2>
      <ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <li
          v-for="principle in GUIDELINE_PRINCIPLES"
          :key="principle.title"
          class="border-default-200 bg-content1 rounded-xl border p-4"
        >
          <div class="flex items-center gap-2">
            <KunIcon :name="principle.icon" class="text-primary-500 size-5" />
            <h3 class="font-semibold">{{ principle.title }}</h3>
          </div>
          <p class="text-default-600 mt-2 text-sm leading-6">
            {{ principle.body }}
          </p>
        </li>
      </ul>
    </section>

    <section class="space-y-4" aria-labelledby="guide-pitfalls">
      <div>
        <h2 id="guide-pitfalls" class="text-lg font-semibold">
          新手最容易出错的地方
        </h2>
        <p class="text-default-500 mt-1 text-sm">
          每一条都链接到详细说明，第一次编辑前花两分钟看一遍。
        </p>
      </div>
      <ul class="grid gap-3 md:grid-cols-2">
        <li v-for="pitfall in GUIDELINE_PITFALLS" :key="pitfall.title">
          <NuxtLink
            :to="pitfall.to"
            class="border-default-200 bg-content1 hover:border-primary-300 group flex h-full items-start gap-3 rounded-xl border p-4 transition-colors"
          >
            <KunIcon
              name="lucide:triangle-alert"
              class="text-warning-500 mt-0.5 size-5 shrink-0"
            />
            <span class="min-w-0">
              <span
                class="group-hover:text-primary-600 block font-medium transition-colors"
              >
                {{ pitfall.title }}
              </span>
              <span class="text-default-600 mt-1 block text-sm leading-6">
                {{ pitfall.body }}
              </span>
            </span>
          </NuxtLink>
        </li>
      </ul>
    </section>

    <section
      v-for="group in groups"
      :key="group.key"
      class="space-y-4"
      :aria-labelledby="`guide-group-${group.key}`"
    >
      <div class="flex items-center gap-2">
        <KunIcon :name="group.icon" class="text-default-500 size-5" />
        <h2 :id="`guide-group-${group.key}`" class="text-lg font-semibold">
          {{ group.label }}
        </h2>
        <span class="text-default-500 text-sm">{{ group.description }}</span>
      </div>
      <ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <li v-for="doc in group.docs" :key="doc.slug">
          <NuxtLink
            :to="`/guidelines/${doc.slug}`"
            class="border-default-200 bg-content1 hover:border-primary-300 group flex h-full flex-col rounded-xl border p-4 transition-colors"
          >
            <span class="flex items-center gap-2">
              <KunIcon :name="doc.icon" class="text-primary-500 size-5" />
              <span
                class="group-hover:text-primary-600 font-semibold transition-colors"
              >
                {{ doc.title }}
              </span>
            </span>
            <span class="text-default-600 mt-2 text-sm leading-6">
              {{ doc.description }}
            </span>
          </NuxtLink>
        </li>
      </ul>
    </section>
  </div>
</template>
