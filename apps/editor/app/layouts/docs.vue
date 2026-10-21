<script setup lang="ts">
const { load } = useEditorSession()
const { capture } = useReturnLink()
const route = useRoute()

const navOpen = ref(false)

watch(
  () => route.fullPath,
  () => {
    navOpen.value = false
  }
)

capture()
callOnce('editor-session', load)
</script>

<template>
  <div class="bg-background text-foreground flex min-h-dvh flex-col">
    <LayoutTopbar @open-nav="navOpen = true" />

    <div class="flex min-h-0 flex-1">
      <aside
        class="border-default-200 bg-content1 sticky top-12 hidden h-[calc(100dvh-3rem)] w-64 shrink-0 border-r md:block"
      >
        <GuidelinesNav />
      </aside>

      <main class="min-w-0 flex-1">
        <slot />
      </main>
    </div>

    <KunDrawer
      v-model="navOpen"
      placement="left"
      :responsive="false"
      size="sm"
      aria-label="编辑规范目录"
      inner-class-name="p-0"
    >
      <GuidelinesNav />
    </KunDrawer>
  </div>
</template>
