<script setup lang="ts">
const emit = defineEmits<{ 'open-nav': [] }>()

const { link } = useReturnLink()
const { isReviewer } = useEditorSession()
</script>

<template>
  <header
    class="border-default-200 bg-content1 sticky top-0 z-30 flex h-12 shrink-0 items-center gap-2 border-b px-2 md:gap-4 md:px-4"
  >
    <KunButton
      variant="light"
      size="sm"
      is-icon-only
      aria-label="打开导航"
      class-name="md:hidden shrink-0"
      @click="emit('open-nav')"
    >
      <KunIcon name="lucide:menu" class="size-5" />
    </KunButton>

    <NuxtLink to="/" class="flex shrink-0 items-center gap-2">
      <img src="/favicon.webp" alt="" class="size-7 rounded-lg" />
      <span class="hidden font-semibold sm:inline">未萌编辑台</span>
    </NuxtLink>

    <div class="flex min-w-0 flex-1 justify-center">
      <LayoutSearchPalette />
    </div>

    <div class="flex shrink-0 items-center gap-1 md:gap-2">
      <KunButton
        v-if="link"
        :href="link.url"
        variant="light"
        size="sm"
        class-name="hidden lg:inline-flex"
      >
        <KunIcon name="lucide:arrow-left" class="size-4" />
        返回{{ link.label }}
      </KunButton>
      <LayoutCreateMenu />
      <LayoutQuotaChip />
      <LayoutReviewInbox v-if="isReviewer" />
      <NotificationsBell />
      <LayoutAccountMenu />
    </div>
  </header>
</template>
